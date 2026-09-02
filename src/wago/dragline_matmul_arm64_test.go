//go:build arm64 && !tinygo

package wago

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDraglineMatmulCorpusMatchesRailshotMemory(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "bench", "corpus", "matmul.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compile := func(config *RuntimeConfig) *Compiled {
		t.Helper()
		compiled, err := Compile(config.WithBoundsChecks(BoundsChecksExplicit), source)
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
	invoke := func(compiled *Compiled, n int32) (uint64, []byte) {
		t.Helper()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		result, err := instance.Invoke("run", I32(n))
		if err != nil || len(result) != 1 {
			t.Fatalf("run(%d) = %v, %v", n, result, err)
		}
		return result[0], append([]byte(nil), instance.Memory().UnsafeBytes()...)
	}
	for _, n := range []int32{-1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 63, 64, 95, 96, 97} {
		wantResult, wantMemory := invoke(reference, n)
		if n == 64 && wantResult != I32(7081204) {
			t.Fatalf("Railshot run(64) = %#x, want corpus oracle %#x", wantResult, I32(7081204))
		}
		for variant, compiled := range native {
			gotResult, gotMemory := invoke(compiled, n)
			if gotResult != wantResult {
				t.Errorf("variant %d run(%d) = %#x, want %#x", variant, n, gotResult, wantResult)
			}
			if !bytes.Equal(gotMemory, wantMemory) {
				for offset := range gotMemory {
					if gotMemory[offset] != wantMemory[offset] {
						t.Errorf("variant %d run(%d) memory first differs at %#x: got %#x, want %#x", variant, n, offset, gotMemory[offset], wantMemory[offset])
						break
					}
				}
			}
		}
	}
}
