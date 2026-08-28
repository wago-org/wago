package modulefile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBoundsModuleInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.wasm")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := Read(path); err == nil || data != nil || !strings.Contains(err.Error(), "CLI limit") {
		t.Fatalf("oversized read = %d bytes, %v", len(data), err)
	}
}
