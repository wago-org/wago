//go:build wago_lean

package wagocli

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCurlGetBytes(t *testing.T) {
	dir := t.TempDir()
	curl := filepath.Join(dir, "curl")
	if err := os.WriteFile(curl, []byte("#!/bin/sh\nprintf test-response\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := curlGetBytes("https://example.invalid/asset")
	if err != nil {
		t.Fatalf("curlGetBytes: %v", err)
	}
	if string(got) != "test-response" {
		t.Fatalf("curlGetBytes = %q, want test-response", got)
	}
}

func TestLeanLatestChannelRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/wago-org/wago/releases" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"tag_name":"canary-cafef00"},{"tag_name":"nightly-20260712-deadbee"}]`))
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	got, err := latestChannelRelease("nightly")
	if err != nil || got != "nightly-20260712-deadbee" {
		t.Fatalf("latestChannelRelease(nightly) = %q, %v", got, err)
	}
}

func TestLeanDownloadNightlyUsesHostAsset(t *testing.T) {
	payload := []byte("fake nightly binary")
	sum := sha256.Sum256(payload)
	asset := versionAsset()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nightly/" + asset:
			_, _ = w.Write(payload)
		case "/nightly/" + asset + ".sha256":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "wago")
	if err := downloadBinary(srv.URL, "nightly", dest); err != nil {
		t.Fatalf("downloadBinary(nightly): %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("downloaded nightly content = %q, %v; want %q, nil", got, err, payload)
	}
}
