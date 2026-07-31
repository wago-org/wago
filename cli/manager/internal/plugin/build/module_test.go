package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildModuleFilesystemAndGoModHelpers(t *testing.T) {
	tmp := t.TempDir()
	goMod := "module example.test/plugin\n\ngo 1.23\n\nreplace example.test/local => ./local\nreplace example.test/remote v1.0.0 => example.test/remote v1.2.0\n"
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "local"), 0o700); err != nil {
		t.Fatal(err)
	}
	if mod, ok := readGoMod(tmp); !ok || mod.Go != "1.23" {
		t.Fatalf("readGoMod = %#v, %v", mod, ok)
	}
	if got := wagoGoDirective(tmp); got != "1.23" {
		t.Fatalf("go directive = %q", got)
	}
	replaces := mirroredReplaces(tmp)
	if len(replaces) != 2 || !strings.Contains(replaces[0], "example.test/local=") || replaces[1] != "example.test/remote@v1.0.0=example.test/remote@v1.2.0" {
		t.Fatalf("mirrored replaces = %#v", replaces)
	}
	for _, path := range []string{".", "..", "./x", "../x", tmp} {
		if !isFilesystemPath(path) {
			t.Fatalf("%q not recognized as filesystem path", path)
		}
	}
	if isFilesystemPath("example.test/mod") || registerImport("example.test/mod") != "example.test/mod/register" || exeSuffix() != "" {
		t.Fatal("module path helpers changed")
	}

	t.Setenv("WAGO_SRC", tmp)
	if got, err := ModuleDir(); err != nil || got != tmp {
		t.Fatalf("WAGO_SRC module dir = %q, %v", got, err)
	}
	if got, ok := SourceDir(); !ok || got != tmp {
		t.Fatalf("WAGO_SRC source dir = %q, %v", got, ok)
	}
	if err := RunGo(tmp, false, "version"); err != nil {
		t.Fatalf("goRun version: %v", err)
	}
}

func TestInstalledSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if dir := InstalledSource(); dir != "" {
		t.Fatalf("no source yet: got %q", dir)
	}
	source := filepath.Join(home, ".wago", "src")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dir := InstalledSource(); dir != "" {
		t.Fatalf("non-wago go.mod: got %q", dir)
	}
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module github.com/wago-org/wago\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dir := InstalledSource(); dir != source {
		t.Fatalf("InstalledSource = %q, want %q", dir, source)
	}
}

func TestEnsureModuleCreatesReusableGoModule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "generated")
	if err := EnsureModule(dir); err != nil {
		t.Fatalf("EnsureModule: %v", err)
	}
	goMod := filepath.Join(dir, "go.mod")
	first, err := os.ReadFile(goMod)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "module "+buildModuleName) {
		t.Fatalf("generated go.mod = %s", first)
	}
	if err := EnsureModule(dir); err != nil {
		t.Fatalf("repeat EnsureModule: %v", err)
	}
	second, err := os.ReadFile(goMod)
	if err != nil || string(second) != string(first) {
		t.Fatalf("repeat go.mod = %q, %v; want unchanged", second, err)
	}
}

func TestSyncModuleRefreshesWagoSource(t *testing.T) {
	buildDir := filepath.Join(t.TempDir(), "generated")
	sourceRoot := t.TempDir()
	first := filepath.Join(sourceRoot, "first")
	second := filepath.Join(sourceRoot, "second")
	for _, source := range []string{first, second} {
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module github.com/wago-org/wago\n\ngo 1.22\n"), 0o644); err != nil {
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
