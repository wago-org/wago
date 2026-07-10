package wagocli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledWagoSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if d := installedWagoSource(); d != "" {
		t.Fatalf("no source yet: want %q, got %q", "", d)
	}
	src := filepath.Join(home, ".wago", "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	// A non-wago go.mod is ignored.
	os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.com/x\n"), 0o644)
	if d := installedWagoSource(); d != "" {
		t.Fatalf("non-wago go.mod: want %q, got %q", "", d)
	}
	// The wago module is found.
	os.WriteFile(filepath.Join(src, "go.mod"), []byte("module github.com/wago-org/wago\n\ngo 1.22\n"), 0o644)
	if d := installedWagoSource(); d != src {
		t.Fatalf("wago source: want %q, got %q", src, d)
	}
}
