//go:build amd64

package amd64

import (
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestModuleTypeCacheBudgetFallback(t *testing.T) {
	n := maxModuleTypeCacheBytes/int(unsafe.Sizeof(wasm.GlobalType{})) + 1
	m := &wasm.Module{Globals: make([]wasm.Global, n)}
	m.Globals[n-1].Type = wasm.GlobalType{Type: wasm.I64, Mutable: true}
	c := buildModuleTypeCache(m, minParallelHintBodyBytes)
	if c.valid || c.memories != nil || c.globals != nil {
		t.Fatal("oversize optional cache retained storage")
	}
	f := fn{m: m, sc: &scratch{moduleTypes: c}}
	if got, ok := f.globalType(uint32(n - 1)); !ok || got != m.Globals[n-1].Type {
		t.Fatalf("fallback = %#v, %v", got, ok)
	}
}

func TestModuleTypeCacheInterleavedImports(t *testing.T) {
	m := &wasm.Module{
		Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 3}}},
		Globals:  []wasm.Global{{Type: wasm.GlobalType{Type: wasm.F64}}},
	}
	for i := 0; i < 64; i++ {
		m.Imports = append(m.Imports,
			wasm.Import{Type: wasm.NewFuncExternType(wasm.TypeIdx{})},
			wasm.Import{Type: wasm.NewMemExternType(wasm.MemType{Limits: wasm.Limits{Min: uint64(i)}})},
			wasm.Import{Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32, Mutable: i%2 == 0})})
	}
	c := buildModuleTypeCache(m, minParallelHintBodyBytes)
	for i, got := range c.memories {
		if want, ok := m.MemoryType(uint32(i)); !ok || got != want {
			t.Fatalf("memory %d = %#v, want %#v", i, got, want)
		}
	}
	for i, got := range c.globals {
		if want, ok := m.GlobalTypeByIndex(uint32(i)); !ok || got != want {
			t.Fatalf("global %d = %#v, want %#v", i, got, want)
		}
	}
}

func BenchmarkBuildModuleTypeCache(b *testing.B) {
	for _, count := range []int{32, 1024, 8192} {
		m := &wasm.Module{Imports: make([]wasm.Import, count)}
		for i := range m.Imports {
			m.Imports[i].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32})
		}
		for _, linear := range []bool{false, true} {
			b.Run(fmt.Sprintf("imports_%d/linear_%v", count, linear), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					var c moduleTypeCache
					if linear {
						c = buildModuleTypeCache(m, minParallelHintBodyBytes)
					} else {
						c = moduleTypeCache{valid: true, globals: make([]wasm.GlobalType, m.GlobalCount())}
						for j := range c.globals {
							c.globals[j], _ = m.GlobalTypeByIndex(uint32(j))
						}
					}
					if len(c.globals) != count {
						b.Fatal("incomplete cache")
					}
				}
			})
		}
	}
}

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
