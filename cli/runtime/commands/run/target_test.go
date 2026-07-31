package run

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksLikeTarget(t *testing.T) {
	for _, name := range []string{"module.wasm", "module.wago"} {
		if !LooksLikeTarget(name) {
			t.Fatalf("%q not recognized as run target", name)
		}
	}
	file := filepath.Join(t.TempDir(), "module")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil || !LooksLikeTarget(file) {
		t.Fatalf("existing file run target = %v", err)
	}
	if LooksLikeTarget(t.TempDir()) || LooksLikeTarget("not-a-command-or-file") {
		t.Fatal("directory or absent file recognized as run target")
	}
}
