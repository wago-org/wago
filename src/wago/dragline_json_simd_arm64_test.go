//go:build arm64 && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDraglineJSONSIMDCorpusPreservedCalleeMatchesRailshot(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "bench", "corpus", "json-as-simd.wasm"))
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
		WithFunctionArtifactCache(NewFunctionArtifactCache(4 << 20))
	native = append(native, compile(cacheConfig), compile(cacheConfig))
	instantiate := func(compiled *Compiled) *Instance {
		t.Helper()
		instance, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
			"env.abort": HostFunc(func(HostModule, []uint64, []uint64) {}),
		}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { instance.Close() })
		if _, err := instance.Invoke("_initialize"); err != nil {
			t.Fatal(err)
		}
		return instance
	}
	referenceInstance := instantiate(reference)
	nativeInstances := make([]*Instance, len(native))
	for i, compiled := range native {
		nativeInstances[i] = instantiate(compiled)
	}
	for _, export := range []string{"serializeN", "deserializeN"} {
		for _, n := range []int32{0, 1, 2, 200} {
			want, err := referenceInstance.Invoke(export, I32(n))
			if err != nil || len(want) != 1 {
				t.Fatalf("Railshot %s(%d) = %v, %v", export, n, want, err)
			}
			for variant, instance := range nativeInstances {
				got, err := instance.Invoke(export, I32(n))
				if err != nil || len(got) != 1 || got[0] != want[0] {
					t.Errorf("variant %d %s(%d) = %v, %v, want %v", variant, export, n, got, err, want)
				}
			}
		}
	}
}
