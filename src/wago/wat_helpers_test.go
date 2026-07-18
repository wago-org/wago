//go:build ((linux && (amd64 || arm64 || riscv64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func watToWasm(t testing.TB, wat string) []byte {
	t.Helper()
	w2w, err := exec.LookPath("wat2wasm")
	if err != nil {
		t.Skip("wat2wasm (wabt) not on PATH")
	}
	dir := t.TempDir()
	src, out := filepath.Join(dir, "m.wat"), filepath.Join(dir, "m.wasm")
	if err := os.WriteFile(src, []byte(wat), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(w2w, src, "-o", out).CombinedOutput(); err != nil {
		t.Fatalf("wat2wasm: %v\n%s", err, output)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func watToWasmCA(t *testing.T, wat string) []byte {
	return watToWasm(t, wat)
}

func requireExternalWAT(*testing.T) bool { return true }
