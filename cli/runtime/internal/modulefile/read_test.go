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

func TestReadStreamReturnsExactSizedResult(t *testing.T) {
	want := "pipe payload"
	got, err := readStream("pipe", strings.NewReader(want))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("readStream = %q, want %q", got, want)
	}
	if cap(got) != len(got) {
		t.Fatalf("result capacity = %d, want exact length %d", cap(got), len(got))
	}
}

func TestReadUsesExactSizedResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "module.wasm")
	want := []byte("wasm payload")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("Read = %q, want %q", got, want)
	}
	if cap(got) != len(got) {
		t.Fatalf("result capacity = %d, want exact length %d", cap(got), len(got))
	}
}
