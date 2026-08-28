package modulefile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
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

func TestInputLimitDistinguishesSourceAndArtifact(t *testing.T) {
	if got := inputLimit([]byte("\x00asm\x01")); got != MaxBytes {
		t.Fatalf("Wasm input limit = %d, want %d", got, MaxBytes)
	}
	if got := inputLimit([]byte("WAGO\x01")); got != MaxArtifactBytes {
		t.Fatalf("artifact input limit = %d, want %d", got, MaxArtifactBytes)
	}
	limits := wago.DefaultArtifactLimits()
	if want := limits.MaxCodeBytes + limits.MaxMetadataBytes + 64; MaxArtifactBytes != want {
		t.Fatalf("artifact input limit = %d, decoder allowance %d", MaxArtifactBytes, want)
	}
}

func TestReadStreamReturnsExactSizedResult(t *testing.T) {
	want := "pipe payload"
	got, err := readStream("pipe", strings.NewReader(want), MaxBytes)
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
