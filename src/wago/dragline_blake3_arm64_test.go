//go:build arm64 && !tinygo

package wago

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDraglineBlake3CorpusMatchesRailshot(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "bench", "corpus", "blake-as.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compile := func(engine CompilerEngine) *Compiled {
		t.Helper()
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(engine).WithTarget(TargetNative), source)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { compiled.Close() })
		return compiled
	}
	reference, native := compile(CompilerRailshot), compile(CompilerDragline)
	invoke := func(compiled *Compiled, n int32) (uint64, []byte) {
		t.Helper()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		if _, err := instance.Invoke("_initialize"); err != nil {
			t.Fatal(err)
		}
		result, err := instance.Invoke("hashN", I32(n))
		if err != nil || len(result) != 1 {
			t.Fatalf("hashN(%d) = %v, %v", n, result, err)
		}
		return result[0], append([]byte(nil), instance.Memory().UnsafeBytes()...)
	}
	for _, n := range []int32{-1, 0, 1, 2, 3, 17, 100} {
		wantResult, wantMemory := invoke(reference, n)
		gotResult, gotMemory := invoke(native, n)
		if gotResult != wantResult {
			t.Errorf("hashN(%d) = %#x, want %#x", n, gotResult, wantResult)
		}
		if !bytes.Equal(gotMemory, wantMemory) {
			t.Errorf("hashN(%d) memory differs", n)
		}
	}
}
