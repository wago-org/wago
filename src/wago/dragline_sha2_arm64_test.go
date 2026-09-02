//go:build arm64

package wago

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDraglineSHA2CorpusMatchesScalarMemory(t *testing.T) {
	if !hostSupportsARM64SHA2() {
		t.Skip("host does not expose ARM64 SHA2")
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "bench", "corpus", "sha256.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compile := func(target CompilerTargetMode) *Compiled {
		t.Helper()
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(target).WithBoundsChecks(BoundsChecksExplicit), source)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { compiled.Close() })
		return compiled
	}
	scalar := compile(TargetCompatibility)
	native := compile(TargetNative)
	if scalar.RequiresARM64SHA2() || !native.RequiresARM64SHA2() {
		t.Fatalf("SHA2 requirements: compatibility=%t native=%t", scalar.RequiresARM64SHA2(), native.RequiresARM64SHA2())
	}
	invoke := func(compiled *Compiled, n int32) (uint64, []byte) {
		t.Helper()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		result, err := instance.Invoke("hashN", I32(n))
		if err != nil || len(result) != 1 {
			t.Fatalf("hashN(%d) = %v, %v", n, result, err)
		}
		return result[0], append([]byte(nil), instance.Memory().UnsafeBytes()...)
	}
	for _, n := range []int32{-1, 0, 1, 8, 64, 65} {
		wantResult, wantMemory := invoke(scalar, n)
		gotResult, gotMemory := invoke(native, n)
		if gotResult != wantResult {
			t.Errorf("hashN(%d) = %#x, want %#x", n, gotResult, wantResult)
		}
		if !bytes.Equal(gotMemory, wantMemory) {
			for offset := range gotMemory {
				if gotMemory[offset] != wantMemory[offset] {
					t.Errorf("hashN(%d) memory first differs at %#x: got %#x, want %#x", n, offset, gotMemory[offset], wantMemory[offset])
					break
				}
			}
		}
	}
}

func TestDraglineSHA2CorpusRequiresExactConstants(t *testing.T) {
	if !hostSupportsARM64SHA2() {
		t.Skip("host does not expose ARM64 SHA2")
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "bench", "corpus", "sha256.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0x98, 0x2f, 0x8a, 0x42, 0x91, 0x44, 0x37, 0x71}
	offset := bytes.Index(source, needle)
	if offset < 0 {
		t.Fatal("SHA-256 round constants not found")
	}
	mutated := append([]byte(nil), source...)
	mutated[offset] ^= 1
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), mutated)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.RequiresARM64SHA2() {
		t.Fatal("mutated constants admitted to SHA2 specialization")
	}
}

func TestDraglineSHA2CorpusArtifactCachePreservesRequirement(t *testing.T) {
	if !hostSupportsARM64SHA2() {
		t.Skip("host does not expose ARM64 SHA2")
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "bench", "corpus", "sha256.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	config := NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).
		WithFunctionArtifactCache(NewFunctionArtifactCache(1 << 20))
	first, err := Compile(config, source)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Compile(config, source)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if !first.RequiresARM64SHA2() || !second.RequiresARM64SHA2() {
		t.Fatalf("SHA2 requirement lost across cache: first=%t second=%t", first.RequiresARM64SHA2(), second.RequiresARM64SHA2())
	}
	if first.CodeSize() != second.CodeSize() {
		t.Fatalf("code size changed across cache: first=%d second=%d", first.CodeSize(), second.CodeSize())
	}
}
