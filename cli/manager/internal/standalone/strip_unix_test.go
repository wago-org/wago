//go:build !windows

package standalone

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStripTinyGoLinuxUsesPortableOptions(t *testing.T) {
	dir := t.TempDir()
	strip := filepath.Join(dir, "strip")
	if err := os.WriteFile(strip, []byte("#!/bin/sh\n[ \"$#\" -eq 2 ] && [ \"$1\" = -s ]\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := stripTinyGo(dir, os.Environ(), false, "program", Target{OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatalf("strip with portable implementation: %v", err)
	}
}
