//go:build arm64

package arm64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAdapterTemplateCacheRequiresRepeatedTypeArm64(t *testing.T) {
	var ft, other wasm.CompType
	code := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	var cache adapterTemplateCache

	cache.observe(&ft, code, 8, 8)
	if _, _, _, ok := cache.lookup(&ft); ok {
		t.Fatal("first adapter shape was cached before it repeated")
	}
	cache.observe(&ft, code, 8, 8)
	got, returnOff, endOff, ok := cache.lookup(&ft)
	if !ok || !bytes.Equal(got, code) || returnOff != 8 || endOff != 8 {
		t.Fatalf("cached adapter = %v, %d, %d, %t", got, returnOff, endOff, ok)
	}
	if _, _, _, ok := cache.lookup(&other); ok {
		t.Fatal("adapter cache matched a different immutable function type")
	}

	code[0] = 99
	got, _, _, _ = cache.lookup(&ft)
	if got[0] != 1 {
		t.Fatal("adapter cache aliases the source function buffer")
	}
}

func TestAdapterTemplateCacheRejectsOversizeShapeArm64(t *testing.T) {
	var ft wasm.CompType
	var cache adapterTemplateCache
	oversize := make([]byte, maxCachedAdapterBytes+1)
	cache.observe(&ft, oversize, 8, len(oversize))
	cache.observe(&ft, oversize, 8, len(oversize))
	if _, _, _, ok := cache.lookup(&ft); ok {
		t.Fatal("oversize adapter shape entered the bounded cache")
	}
}

func TestAdapterTemplateCachePreservesNativeSizeAttributionArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x0b}},
	)
	m.Exports = append(m.Exports,
		wasm.Export{Name: "g", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}},
		wasm.Export{Name: "h", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}},
	)

	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	want := stats.Funcs[1].NativeSize.AdapterToInternalPaddingBytes
	if want == 0 {
		t.Fatal("test adapter shape has no internal-entry padding")
	}
	if got := stats.Funcs[2].NativeSize.AdapterToInternalPaddingBytes; got != want {
		t.Fatalf("cached adapter padding = %d, want %d", got, want)
	}
}
