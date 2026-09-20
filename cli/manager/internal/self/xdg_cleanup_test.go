//go:build linux

package self

import (
	"github.com/wago-org/wago/cli/manager/internal/config"
	"github.com/wago-org/wago/internal/wagopaths"
	"os"
	"path/filepath"
	"testing"
)

func TestFullUninstallIncludesActiveXDGPaths(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		custom, legacy, override bool
	}{
		{name: "default"}, {name: "custom", custom: true}, {name: "legacy", legacy: true},
		{name: "legacy_custom", legacy: true, custom: true}, {name: "wago_home", override: true},
		{name: "wago_home_legacy", override: true, legacy: true, custom: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			setTestHome(t, home)
			t.Setenv("WAGO_HOME", "")
			t.Setenv("WAGO_SRC_DIR", "")
			for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
				value := ""
				if tc.custom {
					value = filepath.Join(home, key)
				}
				t.Setenv(key, value)
			}
			legacy := filepath.Join(home, ".wago")
			if tc.legacy {
				if err := os.MkdirAll(legacy, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if tc.override {
				t.Setenv("WAGO_HOME", filepath.Join(home, "selected"))
			}
			dirs := wagopaths.DirsFor("test")
			targets := Targets(dirs, filepath.Join(home, "bin", "wago"), Full)
			covered := func(path string) bool {
				for _, target := range targets {
					if pathContains(target, path) {
						return true
					}
				}
				return false
			}
			for _, path := range []string{dirs.Data, dirs.Config, filepath.Dir(dirs.Cache)} {
				if !covered(path) {
					t.Errorf("active directory %s absent from %q", path, targets)
				}
				if covered(filepath.Dir(path)) && !tc.override {
					t.Errorf("parent directory included: %s", filepath.Dir(path))
				}
			}
			if tc.legacy && !tc.override && !covered(legacy) {
				t.Errorf("legacy root absent: %q", targets)
			}
			if tc.override && covered(filepath.Join(home, "XDG_CONFIG_HOME", "wago")) {
				t.Errorf("WAGO_HOME did not isolate target paths: %q", targets)
			}
			for i, a := range targets {
				for j, b := range targets {
					if i != j && pathContains(a, b) {
						t.Errorf("duplicate containment: %s contains %s", a, b)
					}
				}
			}
		})
	}
}

func TestFishCompletionInstallCleanupPath(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "custom"}[custom], func(t *testing.T) {
			home := t.TempDir()
			setTestHome(t, home)
			t.Setenv("WAGO_HOME", filepath.Join(home, "wago"))
			t.Setenv("WAGO_SRC_DIR", "")
			root := filepath.Join(home, ".config")
			t.Setenv("XDG_CONFIG_HOME", "")
			if custom {
				root = filepath.Join(home, "config")
				t.Setenv("XDG_CONFIG_HOME", root)
			}
			path, err := config.InstallCompletion("fish", "", "")
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(root, "fish", "completions", "wago.fish")
			if path != want || fishCompletionPath() != want {
				t.Fatalf("install=%s cleanup=%s want=%s", path, fishCompletionPath(), want)
			}
			found := false
			for _, target := range Targets(wagopaths.DirsFor("test"), filepath.Join(home, "bin", "wago"), Full) {
				if target == want {
					found = true
				}
			}
			if !found {
				t.Fatal("installed Fish completion absent from cleanup targets")
			}
		})
	}
}

func TestFullUninstallRemovesLegacySymlinkAndXDGData(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("WAGO_HOME", "")
	t.Setenv("WAGO_SRC_DIR", "")
	for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(home, key))
	}
	dirs := wagopaths.DirsFor("test")
	if err := os.MkdirAll(dirs.Data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs.Data, "managed-data"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(home, ".wago")
	if err := os.Symlink(dirs.Data, legacy); err != nil {
		t.Fatal(err)
	}
	targets := Targets(dirs, filepath.Join(home, "bin", "wago"), Full)
	if err := RemoveManagedPath(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dirs.Data); err != nil {
		t.Fatalf("removing the symlink changed its destination: %v", err)
	}
	for _, target := range targets {
		if !pathContains(home, target) {
			t.Fatalf("test target is outside the temporary home: %s", target)
		}
		if err := removeManagedPathKeepingLock(target, filepath.Join(home, "uninstall.lock")); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{legacy, dirs.Data} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("cleanup left %s: %v; targets=%q", path, err, targets)
		}
	}
}
