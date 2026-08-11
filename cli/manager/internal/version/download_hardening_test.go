package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wago-org/wago/internal/httpclient"
)

func TestParseReleaseChecksumStrict(t *testing.T) {
	asset := "wago-linux-amd64"
	digest := strings.Repeat("a", 64)
	for _, valid := range []string{
		digest + "  " + asset + "\n",
		strings.ToUpper(digest) + " *" + asset + "\r\n",
	} {
		got, err := parseReleaseChecksum([]byte(valid), asset)
		if err != nil || hex.EncodeToString(got[:]) != strings.ToLower(digest) {
			t.Fatalf("valid checksum %q = %x, %v", valid, got, err)
		}
	}
	for name, invalid := range map[string]string{
		"digest only":    digest + "\n",
		"truncated":      digest[:63] + "  " + asset + "\n",
		"non-hex":        strings.Repeat("z", 64) + "  " + asset + "\n",
		"wrong filename": digest + "  other\n",
		"multiple":       digest + "  " + asset + "\n" + digest + "  " + asset + "\n",
		"extra field":    digest + "  " + asset + " extra\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseReleaseChecksum([]byte(invalid), asset); !errors.Is(err, errChecksumFormat) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func FuzzParseReleaseChecksum(f *testing.F) {
	f.Add([]byte(strings.Repeat("0", 64)+"  asset\n"), "asset")
	f.Add([]byte("bad"), "asset")
	f.Fuzz(func(t *testing.T, data []byte, asset string) {
		digest, err := parseReleaseChecksum(data, asset)
		if err == nil {
			if len(data) > int(checksumBodyLimit) {
				t.Fatal("oversized checksum parsed")
			}
			if len(hex.EncodeToString(digest[:])) != 64 {
				t.Fatal("parsed digest has wrong length")
			}
		}
	})
}

func TestDownloadReleaseAssetRejectsOversizedChecksumBeforeAsset(t *testing.T) {
	asset := "wago-test"
	oldMaximum := checksumBodyMaximum
	checksumBodyMaximum = 16
	t.Cleanup(func() { checksumBodyMaximum = oldMaximum })
	var assetRequested atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, ".sha256") {
			writer.(http.Flusher).Flush()
			_, _ = writer.Write([]byte(strings.Repeat("x", 17)))
			return
		}
		assetRequested.Store(true)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "wago")
	if err := os.WriteFile(destination, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := downloadReleaseAssetWithProgressContext(context.Background(), server.URL, "v1", asset, destination, nil)
	if !errors.Is(err, httpclient.ErrBodyTooLarge) {
		t.Fatalf("checksum error = %v", err)
	}
	if assetRequested.Load() {
		t.Fatal("asset was requested before checksum validation")
	}
	data, readErr := os.ReadFile(destination)
	if readErr != nil || string(data) != "old" {
		t.Fatalf("destination = %q, %v", data, readErr)
	}
}

func TestDownloadReleaseAssetStreamsVerifiesAndReplaces(t *testing.T) {
	payload := strings.Repeat("streamed-release-asset", 1<<12)
	asset := "wago-test"
	server := releaseServer(t, asset, []byte(payload), "")
	destination := filepath.Join(t.TempDir(), "wago")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := downloadReleaseAssetWithProgressContext(context.Background(), server.URL, "v1", asset, destination, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != payload {
		t.Fatalf("destination bytes = %d, %v", len(data), err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("destination mode = %v, %v", info, err)
		}
	}
	assertNoDownloadTemps(t, destination)
}

func TestDownloadReleaseAssetFailuresPreserveDestination(t *testing.T) {
	asset := "wago-test"
	payload := []byte(strings.Repeat("payload", 32))
	oldMaximum := releaseAssetMaximum
	releaseAssetMaximum = 128
	t.Cleanup(func() { releaseAssetMaximum = oldMaximum })

	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		cancel  bool
	}{
		{name: "malformed checksum", handler: func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, ".sha256") {
				_, _ = writer.Write([]byte("bad\n"))
				return
			}
			_, _ = writer.Write(payload)
		}},
		{name: "multiple hashes", handler: func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, ".sha256") {
				sum := sha256.Sum256(payload)
				line := fmt.Sprintf("%x  %s\n", sum, asset)
				_, _ = writer.Write([]byte(line + line))
				return
			}
			_, _ = writer.Write(payload)
		}},
		{name: "hash mismatch", handler: func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, ".sha256") {
				_, _ = writer.Write([]byte(strings.Repeat("0", 64) + "  " + asset + "\n"))
				return
			}
			_, _ = writer.Write(payload[:64])
		}},
		{name: "declared oversized", handler: func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, ".sha256") {
				writeChecksum(writer, payload, asset)
				return
			}
			writer.Header().Set("Content-Length", "129")
			writer.WriteHeader(http.StatusOK)
		}},
		{name: "chunked oversized", handler: func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, ".sha256") {
				writeChecksum(writer, payload, asset)
				return
			}
			writer.(http.Flusher).Flush()
			_, _ = writer.Write(make([]byte, 129))
		}},
		{name: "network interruption", handler: func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, ".sha256") {
				writeChecksum(writer, payload[:100], asset)
				return
			}
			writer.Header().Set("Content-Length", "100")
			_, _ = writer.Write(payload[:32])
			writer.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(test.handler))
			defer server.Close()
			directory := t.TempDir()
			destination := filepath.Join(directory, "wago")
			if err := os.WriteFile(destination, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			err := downloadReleaseAssetWithProgressContext(context.Background(), server.URL, "v1", asset, destination, nil)
			if err == nil {
				t.Fatal("download failure was accepted")
			}
			if strings.Contains(test.name, "oversized") && !errors.Is(err, httpclient.ErrBodyTooLarge) {
				t.Fatalf("oversized error = %v", err)
			}
			data, readErr := os.ReadFile(destination)
			if readErr != nil || string(data) != "old" {
				t.Fatalf("old destination = %q, %v (download error %v)", data, readErr, err)
			}
			assertNoDownloadTemps(t, destination)
		})
	}
}

func TestDownloadReleaseAssetCancellationPreservesDestination(t *testing.T) {
	asset := "wago-test"
	payload := []byte(strings.Repeat("x", 256))
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, ".sha256") {
			writeChecksum(writer, payload, asset)
			return
		}
		writer.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = writer.Write(payload[:32])
		writer.(http.Flusher).Flush()
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "wago")
	if err := os.WriteFile(destination, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- downloadReleaseAssetWithProgressContext(ctx, server.URL, "v1", asset, destination, nil)
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled download did not return")
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "old" {
		t.Fatalf("old destination = %q, %v", data, err)
	}
	assertNoDownloadTemps(t, destination)
}

func TestConcurrentReleaseDownloadsDoNotCollide(t *testing.T) {
	asset := "wago-test"
	payload := []byte(strings.Repeat("concurrent", 4096))
	server := releaseServer(t, asset, payload, "")
	destination := filepath.Join(t.TempDir(), "wago")
	const count = 8
	var wait sync.WaitGroup
	errorsChannel := make(chan error, count)
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsChannel <- downloadReleaseAssetWithProgressContext(context.Background(), server.URL, "v1", asset, destination, nil)
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != string(payload) {
		t.Fatalf("destination = %d bytes, %v", len(data), err)
	}
	assertNoDownloadTemps(t, destination)
}

func BenchmarkReleaseDownloadStreaming(b *testing.B) {
	for _, size := range []int{1 << 20, 16 << 20} {
		b.Run(fmt.Sprintf("%dMiB", size>>20), func(b *testing.B) {
			payload := []byte(strings.Repeat("x", size))
			asset := "wago-test"
			server := releaseServer(b, asset, payload, "")
			destination := filepath.Join(b.TempDir(), "wago")
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for range b.N {
				if err := downloadReleaseAssetWithProgressContext(context.Background(), server.URL, "v1", asset, destination, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func releaseServer(t testing.TB, asset string, payload []byte, checksum string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, ".sha256") {
			if checksum != "" {
				_, _ = writer.Write([]byte(checksum))
			} else {
				writeChecksum(writer, payload, asset)
			}
			return
		}
		writer.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = writer.Write(payload)
	}))
	t.Cleanup(server.Close)
	return server
}

func writeChecksum(writer http.ResponseWriter, payload []byte, asset string) {
	sum := sha256.Sum256(payload)
	_, _ = fmt.Fprintf(writer, "%x  %s\n", sum, asset)
}

func assertNoDownloadTemps(t *testing.T, destination string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(destination), ".wago-atomic-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary downloads remain: %v, %v", matches, err)
	}
}
