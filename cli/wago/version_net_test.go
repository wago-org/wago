//go:build !wago_lean

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadBinaryChecksum(t *testing.T) {
	payload := []byte("fake wago binary bytes")
	sum := sha256.Sum256(payload)
	hexsum := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/0.9.0/wago-linux-amd64":
			w.Write(payload)
		case "/0.9.0/wago-linux-amd64.sha256":
			w.Write([]byte(hexsum + "  wago-linux-amd64\n"))
		case "/bad/wago-linux-amd64":
			w.Write(payload)
		case "/bad/wago-linux-amd64.sha256":
			w.Write([]byte("deadbeef  wago-linux-amd64\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "wago")
	if err := downloadBinary(srv.URL, "0.9.0", dest); err != nil {
		t.Fatalf("downloadBinary: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("downloaded content mismatch: %v", err)
	}

	// A checksum mismatch must fail and write nothing.
	badDest := filepath.Join(t.TempDir(), "wago")
	if err := downloadBinary(srv.URL, "bad", badDest); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if _, err := os.Stat(badDest); !os.IsNotExist(err) {
		t.Fatal("checksum mismatch must not write the destination file")
	}
}
