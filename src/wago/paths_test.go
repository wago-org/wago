package wago

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func statDir(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return fi.IsDir(), nil
}

func TestDirsWagoHome(t *testing.T) {
	t.Setenv("WAGO_HOME", "/opt/wagohome")
	d := DirsFor("1.2.3")
	if d.Config != "/opt/wagohome/config" {
		t.Fatalf("Config = %q", d.Config)
	}
	if d.Versions != "/opt/wagohome/data/versions" {
		t.Fatalf("Versions = %q", d.Versions)
	}
	if d.Cache != "/opt/wagohome/cache/1.2.3" {
		t.Fatalf("Cache = %q", d.Cache)
	}
	if got := d.VersionBinary("0.9.0"); got != "/opt/wagohome/data/versions/0.9.0/wago" {
		t.Fatalf("VersionBinary = %q", got)
	}
	if got := d.CachePath("abc"); got != "/opt/wagohome/cache/1.2.3/abc.wago" {
		t.Fatalf("CachePath = %q", got)
	}
}

func TestDirsXDG(t *testing.T) {
	t.Setenv("WAGO_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "/x/config")
	t.Setenv("XDG_CACHE_HOME", "/x/cache")
	t.Setenv("XDG_DATA_HOME", "/x/data")
	d := DirsFor("2.0.0")
	if d.Config != "/x/config/wago" {
		t.Fatalf("Config = %q", d.Config)
	}
	if d.Cache != "/x/cache/wago/2.0.0" {
		t.Fatalf("Cache = %q", d.Cache)
	}
	if d.Versions != "/x/data/wago/versions" {
		t.Fatalf("Versions = %q", d.Versions)
	}
}

func TestDirsHomeFallback(t *testing.T) {
	t.Setenv("WAGO_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/tester")
	d := DirsFor("0.1.0")
	if d.Config != "/home/tester/.config/wago" {
		t.Fatalf("Config = %q", d.Config)
	}
	if d.Cache != "/home/tester/.cache/wago/0.1.0" {
		t.Fatalf("Cache = %q", d.Cache)
	}
	if d.Data != "/home/tester/.local/share/wago" {
		t.Fatalf("Data = %q", d.Data)
	}
}

// Different versions must get isolated caches but share config.
func TestDirsCacheIsolatedConfigShared(t *testing.T) {
	t.Setenv("WAGO_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/tester")
	a, b := DirsFor("0.3.0"), DirsFor("0.5.0")
	if a.Cache == b.Cache {
		t.Fatal("caches must be isolated per version")
	}
	if a.Config != b.Config {
		t.Fatal("config must be shared across versions")
	}
}

func TestDirsEnsure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WAGO_HOME", filepath.Join(root, "wh"))
	d := DirsFor("0.1.0")
	if err := d.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, dir := range []string{d.Config, d.Cache, d.Versions} {
		if !strings.HasPrefix(dir, root) {
			t.Fatalf("unexpected dir outside temp root: %q", dir)
		}
		if fi, err := statDir(dir); err != nil || !fi {
			t.Fatalf("dir %q not created: %v", dir, err)
		}
	}
}
