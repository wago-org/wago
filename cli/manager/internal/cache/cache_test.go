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

func TestCleanBuildsDoesNotFollowVersionDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })
	dirs := wagopaths.Dirs{
		Cache:    filepath.Join(root, "cache", "canary"),
		Versions: filepath.Join(root, "versions"),
		Version:  "canary",
	}
	externalRoot := t.TempDir()
	externalPlugin := filepath.Join(externalRoot, "standard", "normal", "plugins", "keep")
	if err := os.MkdirAll(filepath.Dir(externalPlugin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(externalPlugin, []byte("outside wago"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirs.Versions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalRoot, filepath.Join(dirs.Versions, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := Clean(dirs, Selection{Builds: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(externalPlugin); err != nil {
		t.Fatalf("cache cleanup followed a symlink outside the versions directory: %v", err)
	}
}

func TestLocalBuildCleanupRejectsLinkedDirectory(t *testing.T) {
	for _, link := range []struct {
		name, path, target string
	}{
		{"project root", ".wago", ""},
		{"build root", LocalBuildDir(), "builds"},
	} {
		for _, operation := range []struct {
			name string
			run  func(wagopaths.Dirs) error
		}{
			{"clean", func(dirs wagopaths.Dirs) error {
				_, err := Clean(dirs, Selection{Builds: true})
				return err
			}},
			{"prune", func(dirs wagopaths.Dirs) error {
				_, err := Prune(dirs, time.Hour)
				return err
			}},
		} {
			t.Run(link.name+"/"+operation.name, func(t *testing.T) {
				project := t.TempDir()
				previous, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chdir(project); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chdir(previous) })
				external := t.TempDir()
				marker := filepath.Join(external, "builds", "old", "keep")
				if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(marker, []byte("outside"), 0o600); err != nil {
					t.Fatal(err)
				}
				old := time.Now().Add(-2 * time.Hour)
				if err := os.Chtimes(filepath.Dir(marker), old, old); err != nil {
					t.Fatal(err)
				}
				if link.path == LocalBuildDir() {
					if err := os.Mkdir(".wago", 0o755); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(filepath.Join(external, link.target), link.path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				dirs := wagopaths.Dirs{Cache: filepath.Join(project, "cache", "current"), Versions: filepath.Join(project, "versions"), Version: "current"}
				if err := operation.run(dirs); err == nil {
					t.Errorf("cleanup accepted linked %s", link.path)
				}
				if data, err := os.ReadFile(marker); err != nil || string(data) != "outside" {
					t.Errorf("cleanup changed external file: %q, %v", data, err)
				}
			})
		}
	}
}

func TestCleanBuildsRemovesPluginLeafSymlinkWithoutFollowingIt(t *testing.T) {
	root := t.TempDir()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })
	dirs := wagopaths.Dirs{Versions: filepath.Join(root, "versions")}
	build := filepath.Join(dirs.Versions, "v1", "standard", "normal")
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	marker := filepath.Join(external, "keep")
	if err := os.WriteFile(marker, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(build, "plugins")
	if err := os.Symlink(external, leaf); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	result, err := Clean(dirs, Selection{Builds: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 || result.Bytes != 0 {
		t.Fatalf("removed plugin leaf = %+v, want one zero-byte cache object", result)
	}
	if _, err := os.Lstat(leaf); !os.IsNotExist(err) {
		t.Fatalf("plugin leaf symlink still exists: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "outside" {
		t.Fatalf("plugin leaf symlink target changed: %q, %v", data, err)
	}
}

func TestCleanBuildsRemovesMalformedPluginFileLeaf(t *testing.T) {
	root := t.TempDir()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })
	dirs := wagopaths.Dirs{Versions: filepath.Join(root, "versions")}
	leaf := filepath.Join(dirs.Versions, "v1", "standard", "normal", "plugins")
	if err := os.MkdirAll(filepath.Dir(leaf), 0o755); err != nil {
		t.Fatal(err)
	}
	const data = "malformed cache leaf"
	if err := os.WriteFile(leaf, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Clean(dirs, Selection{Builds: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 || result.Bytes != int64(len(data)) {
		t.Fatalf("removed malformed plugin leaf = %+v", result)
	}
	if _, err := os.Lstat(leaf); !os.IsNotExist(err) {
		t.Fatalf("malformed plugin leaf still exists: %v", err)
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
	artifact := filepath.Join(dirs.Cache, "modules", "ab", "artifact.wago")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("old artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(artifact, old, old); err != nil {
		t.Fatal(err)
	}

	result, err := Prune(dirs, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 2 {
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
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("old artifact still exists: %v", err)
	}
}

func TestFormatBytes(t *testing.T) {
	for value, want := range map[int64]string{0: "0 B", 1024: "1.0 KiB", 1024 * 1024: "1.0 MiB"} {
		if got := FormatBytes(value); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", value, got, want)
		}
	}
}
