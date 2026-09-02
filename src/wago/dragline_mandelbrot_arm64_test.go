//go:build arm64 && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDraglineMandelbrotCorpusMatchesRailshot(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "bench", "corpus", "mandelbrot.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compile := func(config *RuntimeConfig) *Compiled {
		t.Helper()
		compiled, err := Compile(config, source)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { compiled.Close() })
		return compiled
	}
	reference := compile(NewRuntimeConfig().WithCompiler(CompilerRailshot).WithTarget(TargetCompatibility))
	native := []*Compiled{
		compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative)),
		compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithFunctionWorkers(4)),
	}
	cacheConfig := NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).
		WithFunctionArtifactCache(NewFunctionArtifactCache(1 << 20))
	native = append(native, compile(cacheConfig), compile(cacheConfig))
	invoke := func(compiled *Compiled, n int32) uint64 {
		t.Helper()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		result, err := instance.Invoke("render", I32(n))
		if err != nil || len(result) != 1 {
			t.Fatalf("render(%d) = %v, %v", n, result, err)
		}
		return result[0]
	}
	for _, n := range []int32{-2, -1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 15, 16, 31, 32, 63, 64, 65, 96} {
		want := invoke(reference, n)
		if n == 64 && want != I32(108048) {
			t.Fatalf("Railshot render(64) = %#x, want Wasmtime oracle %#x", want, I32(108048))
		}
		for variant, compiled := range native {
			if got := invoke(compiled, n); got != want {
				t.Errorf("variant %d render(%d) = %#x, want %#x", variant, n, got, want)
			}
		}
	}
}
