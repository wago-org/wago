//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAdapterTemplateEmissionAndGCMetadataParityArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	build := func(repeated bool) *wasm.Module {
		m := modFuncs(t,
			funcDef{i32, i32, []byte{0, 0x20, 0, 0x0b}},
			funcDef{i32, i32, []byte{0, 0x20, 0, 0x0b}},
			funcDef{i32, i32, []byte{0, 0x20, 0, 0x0b}},
			funcDef{i32, i32, []byte{0, 0x20, 0, 0x0b}},
		)
		for i := range m.Code {
			m.Exports = append(m.Exports, wasm.Export{Name: string(rune('a' + i)), Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: uint32(i)}})
			if repeated {
				m.FuncTypes[i] = wasm.TypeIdx{}
			}
		}
		return m
	}
	plans := func() *shared.GCModuleFrameRootPlan {
		return testGCModuleRootPlansARM64(t,
			&shared.GCFrameRootPlan{Candidate: true}, &shared.GCFrameRootPlan{Candidate: true},
			&shared.GCFrameRootPlan{Candidate: true}, &shared.GCFrameRootPlan{Candidate: true})
	}
	cachedRoots, uncachedRoots := plans(), plans()
	cached, err := CompileModuleWith(build(true), CompileOptions{Workers: 1, GCFrameRoots: cachedRoots})
	if err != nil {
		t.Fatal(err)
	}
	defer cached.CodeImage.Close()
	uncached, err := CompileModuleWith(build(false), CompileOptions{Workers: 1, GCFrameRoots: uncachedRoots})
	if err != nil {
		t.Fatal(err)
	}
	defer uncached.CodeImage.Close()
	if !bytes.Equal(cached.Code, uncached.Code) || !reflect.DeepEqual(cached.Entry, uncached.Entry) || !reflect.DeepEqual(cached.InternalEntry, uncached.InternalEntry) {
		t.Fatal("reused adapter changed emitted bytes or entries")
	}
	for i := range cached.Entry {
		got, want := cachedRoots.Function(i), uncachedRoots.Function(i)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("function %d GC metadata differs: %#v / %#v", i, got, want)
		}
		if got.AdapterReturnOffset < 4 {
			t.Fatalf("function %d has no adapter return", i)
		}
		call := cached.Entry[i] + int(got.AdapterReturnOffset) - 4
		word := binary.LittleEndian.Uint32(cached.Code[call:])
		displacement := int(int32(word<<6)>>6) * 4
		if word>>26 != 0x25 || call+displacement != cached.InternalEntry[i] {
			t.Fatalf("function %d adapter call targets %d, want %d", i, call+displacement, cached.InternalEntry[i])
		}
	}
}

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
