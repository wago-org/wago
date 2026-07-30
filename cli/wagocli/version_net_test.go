//go:build !wago_lean

package wagocli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/wago-org/wago/internal/wagopaths"
)

func TestLatestChannelRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/wago-org/wago/releases" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"tag_name":"nightly-20260712-deadbee"},{"tag_name":"canary-cafef00"}]`))
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	got, err := latestChannelRelease("nightly")
	if err != nil || got != "nightly-20260712-deadbee" {
		t.Fatalf("latestChannelRelease(nightly) = %q, %v", got, err)
	}
}

func TestHTTPGetBytesReportsDownloadProgress(t *testing.T) {
	payload := []byte("progress payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	var current, total int64
	got, err := httpGetBytesProgress(srv.URL, func(c, t int64) {
		current, total = c, t
	})
	if err != nil || string(got) != string(payload) {
		t.Fatalf("httpGetBytesProgress = %q, %v", got, err)
	}
	if current != int64(len(payload)) || total != int64(len(payload)) {
		t.Fatalf("download progress = %d/%d, want %d/%d", current, total, len(payload), len(payload))
	}
}

func TestDownloadBinaryChecksum(t *testing.T) {
	payload := []byte("fake wago binary bytes")
	sum := sha256.Sum256(payload)
	hexsum := hex.EncodeToString(sum[:])
	asset := "wago-runtime-standard-normal-" + runtime.GOOS + "-" + runtime.GOARCH

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.9.0/" + asset, "/nightly/" + asset, "/canary/" + asset:
			w.Write(payload)
		case "/v0.9.0/" + asset + ".sha256", "/nightly/" + asset + ".sha256", "/canary/" + asset + ".sha256":
			w.Write([]byte(hexsum + "  " + asset + "\n"))
		case "/bad/" + asset:
			w.Write(payload)
		case "/bad/" + asset + ".sha256":
			w.Write([]byte("deadbeef  " + asset + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "wago")
	if err := downloadBinary(srv.URL, canonicalReleaseRef("0.9.0"), wagopaths.ProfileStandard, wagopaths.BuildNormal, dest); err != nil {
		t.Fatalf("downloadBinary: %v", err)
	}

	for _, channel := range []string{"nightly", "canary"} {
		dest := filepath.Join(t.TempDir(), "wago")
		if err := downloadBinary(srv.URL, channel, wagopaths.ProfileStandard, wagopaths.BuildNormal, dest); err != nil {
			t.Fatalf("downloadBinary(%q): %v", channel, err)
		}
		got, err := os.ReadFile(dest)
		if err != nil || string(got) != string(payload) {
			t.Fatalf("downloaded %s content mismatch: %v", channel, err)
		}
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("downloaded content mismatch: %v", err)
	}

	// A checksum mismatch must fail and write nothing.
	badDest := filepath.Join(t.TempDir(), "wago")
	if err := downloadBinary(srv.URL, "bad", wagopaths.ProfileStandard, wagopaths.BuildNormal, badDest); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if _, err := os.Stat(badDest); !os.IsNotExist(err) {
		t.Fatal("checksum mismatch must not write the destination file")
	}
}

func TestVersionAssetsIncludeProfileAndHost(t *testing.T) {
	for _, profile := range wagopaths.Profiles {
		for _, build := range wagopaths.Builds {
			want := "wago-runtime-" + string(profile) + "-" + string(build) + "-" + runtime.GOOS + "-" + runtime.GOARCH
			if got := versionAsset(profile, build); got != want {
				t.Fatalf("versionAsset(%s, %s) = %q, want %q", profile, build, got, want)
			}
		}
	}
}

func TestCanonicalReleaseRef(t *testing.T) {
	for input, want := range map[string]string{
		"0.2.0":                   "v0.2.0",
		"v0.2.0":                  "v0.2.0",
		"main":                    "main",
		"canary-20260729-deadbee": "canary-20260729-deadbee",
	} {
		if got := canonicalReleaseRef(input); got != want {
			t.Fatalf("canonicalReleaseRef(%q) = %q, want %q", input, got, want)
		}
	}
}
