package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallRootUsesTinyGoActionArchitecture(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "amd64"},
		{"linux", "arm64", "aarch64"},
		{"darwin", "amd64", "x86_64"},
		{"darwin", "arm64", "arm64"},
		{"windows", "amd64", "amd64"},
	} {
		got, err := installRoot("/tool-cache", "0.41.1", tc.goos, tc.goarch)
		if err != nil || !strings.HasSuffix(filepath.ToSlash(got), "/tinygo/0.41.1/"+tc.want) {
			t.Errorf("installRoot(%s/%s) = %q, %v", tc.goos, tc.goarch, got, err)
		}
	}
}

func TestVerifiedTinyGoCacheDetectsChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture executable uses a shell script")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "tinygo", "bin", "tinygo")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTinyGoFixture(t, bin)
	const goVersion = "go1.test"
	if err := record(root, "0.41.1", goVersion); err != nil {
		t.Fatal(err)
	}
	if err := verify(root, "0.41.1", goVersion, true); err != nil {
		t.Fatalf("valid cache failed verification: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tinygo", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tinygo", "lib", "changed"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verify(root, "0.41.1", goVersion, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("tampered cache still exists: %v", err)
	}
}

func writeTinyGoFixture(t *testing.T, path string) {
	t.Helper()
	contents := "#!/bin/sh\nprintf 'tinygo version 0.41.1 test\\n'\n"
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
