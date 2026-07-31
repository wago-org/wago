package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wago-org/wago/internal/wagopaths"
)

func TestCleanRemovesOnlySelectedCacheLocations(t *testing.T) {
	root := t.TempDir()
	dirs := wagopaths.Dirs{
		Cache:    filepath.Join(root, "cache", "canary"),
		Versions: filepath.Join(root, "versions"),
		Version:  "canary",
	}
	download := filepath.Join(DownloadDir(dirs), "artifact")
	pluginBuild := filepath.Join(dirs.Versions, "canary", "standard", "normal", "plugins", "binary")
	for _, path := range []string{download, pluginBuild} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("cache"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Clean(dirs, Selection{Downloads: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 || result.Bytes != 5 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(download); !os.IsNotExist(err) {
		t.Fatalf("download cache still exists: %v", err)
	}
	if _, err := os.Stat(pluginBuild); err != nil {
		t.Fatalf("plugin build was removed: %v", err)
	}
}

func TestPruneKeepsInstalledAndCurrentCaches(t *testing.T) {
	root := t.TempDir()
	dirs := wagopaths.Dirs{
		Cache:    filepath.Join(root, "cache", "canary"),
		Versions: filepath.Join(root, "versions"),
		Version:  "canary",
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, name := range []string{"canary", "installed", "unused"} {
		path := filepath.Join(DownloadDir(dirs), name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dirs.Versions, "installed"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := Prune(dirs, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 {
		t.Fatalf("removed = %d", result.Removed)
	}
	for _, name := range []string{"canary", "installed"} {
		if _, err := os.Stat(filepath.Join(DownloadDir(dirs), name)); err != nil {
			t.Fatalf("%s cache was removed: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(DownloadDir(dirs), "unused")); !os.IsNotExist(err) {
		t.Fatalf("unused cache still exists: %v", err)
	}
}

func TestFormatBytes(t *testing.T) {
	for value, want := range map[int64]string{0: "0 B", 1024: "1.0 KiB", 1024 * 1024: "1.0 MiB"} {
		if got := FormatBytes(value); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", value, got, want)
		}
	}
}
