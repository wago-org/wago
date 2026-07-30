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

	"github.com/wago-org/wago/internal/wagopaths"
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

func TestLeanMainCommitBrowsing(t *testing.T) {
	const sha = "deadbee123456789012345678901234567890123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/wago-org/wago/commits/main":
			_, _ = w.Write([]byte(`{"sha":"` + sha + `"}`))
		case "/repos/wago-org/wago/commits":
			_, _ = w.Write([]byte(`[{"sha":"` + sha + `","commit":{"author":{"date":"2026-07-29T12:00:00Z"}}}]`))
		case "/repos/wago-org/wago/releases":
			_, _ = w.Write([]byte(`[{"tag_name":"v0.2.0"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	if got, err := latestMainCommit(); err != nil || got != sha {
		t.Fatalf("latestMainCommit = %q, %v", got, err)
	}
	commits, err := fetchMainCommits()
	if err != nil || len(commits) != 1 || commits[0].SHA != sha {
		t.Fatalf("fetchMainCommits = %#v, %v", commits, err)
	}
	releases, err := fetchReleases()
	if err != nil || len(releases) != 1 || releases[0].TagName != "v0.2.0" {
		t.Fatalf("fetchReleases = %#v, %v", releases, err)
	}
}

func TestLeanDownloadNightlyUsesHostAsset(t *testing.T) {
	payload := []byte("fake nightly binary")
	sum := sha256.Sum256(payload)
	asset := versionAsset(wagopaths.ProfileMinimal, wagopaths.BuildTiny)

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
	if err := downloadBinary(srv.URL, "nightly", wagopaths.ProfileMinimal, wagopaths.BuildTiny, dest); err != nil {
		t.Fatalf("downloadBinary(nightly): %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("downloaded nightly content = %q, %v; want %q, nil", got, err, payload)
	}
}
