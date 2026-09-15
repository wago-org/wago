package self

import (
	"context"
	"github.com/wago-org/wago/internal/filelock"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/internal/managedrelease"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestManagedReleaseUninstallRemovesAllPairs(t *testing.T) {
	for _, mode := range []Mode{Minimal, Partial, Full} {
		for _, location := range []string{"default", "shared-custom"} {
			t.Run(string(mode)+"/"+location, func(t *testing.T) {
				home := t.TempDir()
				setTestHome(t, home)
				root := filepath.Join(home, ".wago")
				t.Setenv("WAGO_HOME", root)
				t.Setenv("WAGO_SRC_DIR", "")
				bin := filepath.Join(root, "bin")
				if location == "shared-custom" {
					bin = filepath.Join(home, "shared-bin")
				}
				launcher := filepath.Join(bin, "wago")
				var latest *managedrelease.Release
				for _, version := range []string{"old", "current"} {
					r, err := managedrelease.Prepare(launcher, version, func(binary, source string) error {
						if err := os.MkdirAll(source, 0755); err != nil {
							return err
						}
						if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module fixture"), 0644); err != nil {
							return err
						}
						return os.WriteFile(binary, []byte("manager"), 0755)
					}, func(string) error { return nil })
					if err != nil {
						t.Fatal(err)
					}
					if err := managedrelease.Publish(r, nil, nil); err != nil {
						t.Fatal(err)
					}
					latest = r
				}
				// Use the platform launcher name selected by the managed payload.
				launcher = managedrelease.Launcher(latest.Binary())
				if err := os.WriteFile(launcher, []byte("dispatcher"), 0755); err != nil {
					t.Fatal(err)
				}
				dirs := wagopaths.Dirs{Data: root, Config: filepath.Join(root, "config"), Versions: filepath.Join(root, "versions"), Cache: filepath.Join(root, "cache", "current")}
				runtimeFile := filepath.Join(dirs.Versions, "runtime")
				if err := os.MkdirAll(dirs.Versions, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(runtimeFile, []byte("runtime"), 0755); err != nil {
					t.Fatal(err)
				}
				sibling := filepath.Join(bin, "other-tool")
				if location == "shared-custom" {
					if err := os.WriteFile(sibling, []byte("keep"), 0755); err != nil {
						t.Fatal(err)
					}
				}
				// Accept a running payload path as well as the stable launcher.
				selfUninstall(dirs, latest.Binary(), mode, true, strings.NewReader(""), io.Discard)
				for _, path := range []string{launcher, filepath.Join(bin, ".wago-release.json"), filepath.Join(bin, ".wago-release.lock"), filepath.Join(bin, ".wago-releases")} {
					if _, err := os.Lstat(path); !os.IsNotExist(err) {
						t.Errorf("managed artifact survived: %s: %v", path, err)
					}
				}
				if mode == Minimal {
					if _, err := os.Stat(runtimeFile); err != nil {
						t.Errorf("minimal uninstall removed runtime: %v", err)
					}
				}
				if location == "shared-custom" {
					if data, err := os.ReadFile(sibling); err != nil || string(data) != "keep" {
						t.Errorf("unrelated sibling changed: %q, %v", data, err)
					}
				}
			})
		}
	}
}

func TestManagedUninstallWaitsForPublication(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	root := filepath.Join(home, ".wago")
	t.Setenv("WAGO_HOME", root)
	t.Setenv("WAGO_SRC_DIR", "")
	launcher := filepath.Join(root, "bin", "wago")
	lockPath := managedrelease.PublicationLockPath(launcher)
	lock, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.WriteFile(launcher, []byte("manager"), 0755); err != nil {
		t.Fatal(err)
	}
	dirs := wagopaths.Dirs{Data: root, Config: filepath.Join(root, "config"), Versions: filepath.Join(root, "versions"), Cache: filepath.Join(root, "cache", "current")}
	done := make(chan struct{})
	go func() { selfUninstall(dirs, launcher, Full, true, strings.NewReader(""), io.Discard); close(done) }()
	select {
	case <-done:
		t.Fatal("uninstall bypassed active publisher")
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := os.Stat(launcher); err != nil {
		t.Fatalf("uninstall removed manager before locking: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("uninstall did not resume")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("installation survived: %v", err)
	}
}

func TestRetiredCleanupPreservesNewInstallation(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, ".wago-release.lock")
	lock, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.WriteFile(filepath.Join(root, "old"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedPathKeepingLock(root, lockPath); err != nil {
		t.Fatal(err)
	}
	if err := lock.Retire(lockPath); err != nil {
		t.Fatal(err)
	}
	fresh, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	manager := filepath.Join(root, "new")
	if err := os.WriteFile(manager, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeEmptyInstallationDir(root); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(manager); err != nil || string(data) != "new" {
		t.Fatalf("new installation changed: %q, %v", data, err)
	}
}
