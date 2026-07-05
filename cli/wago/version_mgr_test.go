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

	"github.com/wago-org/wago"
)

// installFake writes a fake installed binary for ver under d.
func installFake(t *testing.T, d wago.Dirs, ver string) {
	t.Helper()
	dir := filepath.Join(d.Versions, ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.VersionBinary(ver), []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestVersionManagerState(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	d := wago.DirsFor("test")

	if got := installedVersions(d); len(got) != 0 {
		t.Fatalf("expected no versions, got %v", got)
	}
	installFake(t, d, "0.3.0")
	installFake(t, d, "0.5.0")
	installFake(t, d, "0.10.0")

	got := installedVersions(d)
	want := []string{"0.3.0", "0.5.0", "0.10.0"} // numeric semver order, not lexical
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("installedVersions = %v, want %v", got, want)
	}

	if activeVersion(d) != "" {
		t.Fatal("expected no active version")
	}
	if err := setActiveVersion(d, "0.5.0"); err != nil {
		t.Fatalf("setActiveVersion: %v", err)
	}
	if activeVersion(d) != "0.5.0" {
		t.Fatalf("activeVersion = %q, want 0.5.0", activeVersion(d))
	}
}

func TestDownloadBinaryChecksum(t *testing.T) {
	payload := []byte("fake wago binary bytes")
	sum := sha256.Sum256(payload)
	hexsum := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/0.9.0/wago-linux-amd64":
			w.Write(payload)
		case r.URL.Path == "/0.9.0/wago-linux-amd64.sha256":
			w.Write([]byte(hexsum + "  wago-linux-amd64\n"))
		case r.URL.Path == "/bad/wago-linux-amd64":
			w.Write(payload)
		case r.URL.Path == "/bad/wago-linux-amd64.sha256":
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

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.5.0", -1},
		{"0.10.0", "0.9.0", 1},
		{"1.0.0", "1.0.0", 0},
		{"v1.2.0", "1.2.0", 0},
		{"1.2.0", "1.2.1", -1},
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Fatalf("compareSemver(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
