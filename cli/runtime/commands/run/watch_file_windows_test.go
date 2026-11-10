//go:build windows && !tinygo && !wago_lean

package run

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenWatchedFileAllowsReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "module.wasm")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openWatchedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove open watched file: %v", err)
	}
}
