package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/wagopaths"
)

func TestCleanPickerShowsSizesAndSelectsEverythingByDefault(t *testing.T) {
	root := t.TempDir()
	dirs := wagopaths.Dirs{
		Cache:    filepath.Join(root, "cache", "canary"),
		Versions: filepath.Join(root, "versions"),
		Version:  "canary",
	}
	writeSizedFile(t, filepath.Join(DownloadDir(dirs), "artifact"), 1024)
	writeSizedFile(t, filepath.Join(dirs.Versions, "canary", "standard", "normal", "plugins", "wago"), 2048)

	picker, err := CleanPicker(dirs)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(picker.Chosen(), ","); got != "Downloads,Builds" {
		t.Fatalf("default selection = %q", got)
	}
	frame := picker.Frame()
	for _, want := range []string{"Choose caches to clean", "Downloads", "1.0 KiB", "Builds", "2.0 KiB", "space toggle", "a toggle all"} {
		if !strings.Contains(frame, want) {
			t.Errorf("picker does not contain %q:\n%s", want, frame)
		}
	}
}

func writeSizedFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}
