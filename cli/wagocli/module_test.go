package wagocli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModuleInspectionCompilesAndReportsEmptyModule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.wasm")
	if err := os.WriteFile(path, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	rt := runtimeWithAllPlugins()
	defer rt.Close()
	mod := compileForInspect(rt, path)
	defer mod.Close()
	if got := mod.Imports(); len(got) != 0 {
		t.Fatalf("imports = %#v, want none", got)
	}
	if got := mod.RequiredCapabilities(); len(got) != 0 {
		t.Fatalf("capabilities = %#v, want none", got)
	}

	// These commands must accept a valid module with no imports or capabilities.
	moduleImports(path)
	moduleCapabilities(path)
}

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
	if got, err := wagoModuleDir(); err != nil || got != tmp {
		t.Fatalf("WAGO_SRC module dir = %q, %v", got, err)
	}
	if got, ok := wagoSourceDir(); !ok || got != tmp {
		t.Fatalf("WAGO_SRC source dir = %q, %v", got, ok)
	}
	if err := goRun(tmp, false, "version"); err != nil {
		t.Fatalf("goRun version: %v", err)
	}
}

func TestValidateModuleBytes(t *testing.T) {
	valid := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	if err := validateModuleBytes(valid); err != nil {
		t.Fatalf("valid empty module: %v", err)
	}
	if err := validateModuleBytes([]byte("not wasm")); err == nil || !strings.Contains(err.Error(), "decode:") {
		t.Fatalf("malformed module error = %v", err)
	}
}
