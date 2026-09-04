//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBuildModuleTypeCacheIsBoundedAndPreservesIndexOrder(t *testing.T) {
	importMemory := wasm.MemType{Limits: wasm.Limits{Min: 1, HasMax: true, Max: 2}}
	localMemory := wasm.MemType{Limits: wasm.Limits{Min: 3, Addr64: true}}
	importGlobal := wasm.GlobalType{Type: wasm.I64, Mutable: true}
	localGlobal := wasm.GlobalType{Type: wasm.F32}
	m := &wasm.Module{
		Imports: []wasm.Import{
			{Type: wasm.NewGlobalExternType(importGlobal)},
			{Type: wasm.NewMemExternType(importMemory)},
		},
		Memories: []wasm.MemType{localMemory},
		Globals:  []wasm.Global{{Type: localGlobal}},
	}

	if got := buildModuleTypeCache(m, minParallelHintBodyBytes-1); got.valid {
		t.Fatal("small module unexpectedly retained a type cache")
	}
	c := buildModuleTypeCache(m, minParallelHintBodyBytes)
	if !c.valid {
		t.Fatal("large module did not retain a type cache")
	}
	if len(c.memories) != 2 || c.memories[0] != importMemory || c.memories[1] != localMemory {
		t.Fatalf("memory cache = %#v, want [%#v %#v]", c.memories, importMemory, localMemory)
	}
	if len(c.globals) != 2 || c.globals[0] != importGlobal || c.globals[1] != localGlobal {
		t.Fatalf("global cache = %#v, want [%#v %#v]", c.globals, importGlobal, localGlobal)
	}
}

func TestModuleTypeCacheLookupFallsBackAndRejectsOutOfRange(t *testing.T) {
	memory := wasm.MemType{Limits: wasm.Limits{Min: 2}}
	global := wasm.GlobalType{Type: wasm.I32, Mutable: true}
	m := &wasm.Module{
		Memories: []wasm.MemType{memory},
		Globals:  []wasm.Global{{Type: global}},
	}
	f := fn{m: m}
	if got, ok := f.memoryType(0); !ok || got != memory {
		t.Fatalf("fallback memory lookup = %#v, %v", got, ok)
	}
	if got, ok := f.globalType(0); !ok || got != global {
		t.Fatalf("fallback global lookup = %#v, %v", got, ok)
	}

	f.sc = &scratch{moduleTypes: buildModuleTypeCache(m, minParallelHintBodyBytes)}
	if _, ok := f.memoryType(1); ok {
		t.Fatal("cached out-of-range memory lookup succeeded")
	}
	if _, ok := f.globalType(1); ok {
		t.Fatal("cached out-of-range global lookup succeeded")
	}
}
