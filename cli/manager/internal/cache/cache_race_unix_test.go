//go:build linux || darwin

package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/wagopaths"
	"golang.org/x/sys/unix"
)

func TestPluginBuildRemovalKeepsOpenedDirectoryAfterPathSwap(t *testing.T) {
	root := t.TempDir()
	versions := filepath.Join(root, "versions")
	plugin := filepath.Join(versions, "v1", "standard", "normal", "plugins")
	if err := os.MkdirAll(plugin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugin, "cached"), []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	externalPlugin := filepath.Join(external, "standard", "normal", "plugins")
	if err := os.MkdirAll(externalPlugin, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(externalPlugin, "keep")
	if err := os.WriteFile(marker, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	rootFD, err := unix.Open(versions, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	rootDir := os.NewFile(uintptr(rootFD), versions)
	defer rootDir.Close()
	versionDir, err := openDirAt(rootDir, "v1")
	if err != nil {
		t.Fatal(err)
	}
	defer versionDir.Close()
	profileDir, err := openDirAt(versionDir, "standard")
	if err != nil {
		t.Fatal(err)
	}
	defer profileDir.Close()
	buildDir, err := openDirAt(profileDir, "normal")
	if err != nil {
		t.Fatal(err)
	}
	defer buildDir.Close()
	pluginDir, err := openDirAt(buildDir, "plugins")
	if err != nil {
		t.Fatal(err)
	}

	parked := filepath.Join(versions, "parked")
	if err := os.Rename(filepath.Join(versions, "v1"), parked); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(versions, "v1")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	bytes, err := removeOpenDir(pluginDir)
	closeErr := pluginDir.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("handle-relative cleanup = %v, close = %v", err, closeErr)
	}
	if bytes != int64(len("cache")) {
		t.Fatalf("removed bytes = %d", bytes)
	}
	if err := unix.Unlinkat(int(buildDir.Fd()), "plugins", unix.AT_REMOVEDIR); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "outside" {
		t.Fatalf("path swap escaped versions tree: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(parked, "standard", "normal", "plugins")); !os.IsNotExist(err) {
		t.Fatalf("opened plugin cache still exists: %v", err)
	}
}

func TestMeasureBuildsDoesNotFollowVersionSymlink(t *testing.T) {
	root := t.TempDir()
	versions := filepath.Join(root, "versions")
	external := t.TempDir()
	plugin := filepath.Join(external, "standard", "normal", "plugins")
	if err := os.MkdirAll(plugin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugin, "outside"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(versions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(versions, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })
	bytes, err := Measure(wagopaths.Dirs{Versions: versions}, Selection{Builds: true})
	if err != nil || bytes != 0 {
		t.Fatalf("contained build size = %d, %v", bytes, err)
	}
}
