package wagocli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
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

func TestBuildModuleLocationAndSourceSelectionHelpers(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	local, err := buildDirFor(false)
	wantLocal := filepath.Join(currentDir, ".wago", "builds", versionString(), "standard", "normal")
	if err != nil || local != wantLocal {
		t.Fatalf("local buildDirFor = %q, %v", local, err)
	}
	global, err := buildDirFor(true)
	dirs := wago.DirsFor(versionString())
	wantGlobal := filepath.Join(dirs.Versions, versionString(), "standard", "normal", "plugins")
	if err != nil || global != wantGlobal {
		t.Fatalf("global buildDirFor = %q, %v; want %q", global, err, wantGlobal)
	}
	if source, err := depsSource(false); err != nil || source != "." {
		t.Fatalf("local depsSource = %q, %v", source, err)
	}
	if source, err := depsSource(true); err != nil || source != dirs.Data {
		t.Fatalf("global depsSource = %q, %v; want %q", source, err, dirs.Data)
	}
	if got := registerImport("example.com/plugin"); got != "example.com/plugin/register" {
		t.Fatalf("registerImport = %q", got)
	}
	src := filepath.Join(dir, "source")
	t.Setenv("WAGO_SRC", src)
	if got, err := wagoModuleDir(); err != nil || got != src {
		t.Fatalf("WAGO_SRC module dir = %q, %v", got, err)
	}
}

func TestEnsureBuildModuleCreatesReusableGoModule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "generated")
	if err := ensureBuildModule(dir); err != nil {
		t.Fatalf("ensureBuildModule: %v", err)
	}
	goMod := filepath.Join(dir, "go.mod")
	first, err := os.ReadFile(goMod)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "module "+buildModuleName) {
		t.Fatalf("generated go.mod = %s", first)
	}
	if err := ensureBuildModule(dir); err != nil {
		t.Fatalf("repeat ensureBuildModule: %v", err)
	}
	second, err := os.ReadFile(goMod)
	if err != nil || string(second) != string(first) {
		t.Fatalf("repeat go.mod = %q, %v; want unchanged", second, err)
	}
}

func TestSyncBuildModuleRefreshesWagoSource(t *testing.T) {
	buildDir := filepath.Join(t.TempDir(), "generated")
	sourceRoot := t.TempDir()
	first := filepath.Join(sourceRoot, "first")
	second := filepath.Join(sourceRoot, "second")
	for _, source := range []string{first, second} {
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(source, "go.mod"),
			[]byte("module github.com/wago-org/wago\n\ngo 1.22\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("WAGO_SRC", first)
	changed, err := syncBuildModule(buildDir)
	if err != nil || !changed {
		t.Fatalf("initial sync = changed %v, err %v", changed, err)
	}
	body, err := os.ReadFile(filepath.Join(buildDir, "go.mod"))
	if err != nil || !strings.Contains(string(body), filepath.ToSlash(first)) {
		t.Fatalf("initial go.mod = %q, %v", body, err)
	}

	changed, err = syncBuildModule(buildDir)
	if err != nil || changed {
		t.Fatalf("repeat sync = changed %v, err %v", changed, err)
	}

	t.Setenv("WAGO_SRC", second)
	changed, err = syncBuildModule(buildDir)
	if err != nil || !changed {
		t.Fatalf("source refresh = changed %v, err %v", changed, err)
	}
	body, err = os.ReadFile(filepath.Join(buildDir, "go.mod"))
	if err != nil || !strings.Contains(string(body), filepath.ToSlash(second)) ||
		strings.Contains(string(body), filepath.ToSlash(first)) {
		t.Fatalf("refreshed go.mod = %q, %v", body, err)
	}
}
