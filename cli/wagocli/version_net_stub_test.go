//go:build wago_lean

package wagocli

import (
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
