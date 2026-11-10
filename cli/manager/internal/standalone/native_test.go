package standalone

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyRuntimeSourcesIsolated(t *testing.T) {
	source := t.TempDir()
	target := filepath.Join(t.TempDir(), "runtime")
	for name, body := range map[string]string{"go.mod": "module test\n", "src/native.go": "package native\n", "src/native_test.go": "test", ".worktrees/other/data.go": "private", "corpus/big.wasm": "large", "secret.env": "private"} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyRuntimeSources(source, target); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go.mod", "src/native.go"} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"src/native_test.go", ".worktrees", "corpus", "secret.env"} {
		if _, err := os.Stat(filepath.Join(target, name)); !os.IsNotExist(err) {
			t.Fatalf("copied non-runtime input %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, "src/native.go"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(source, "src/native.go"))
	if err != nil || string(original) != "package native\n" {
		t.Fatal("runtime source was modified")
	}
}
