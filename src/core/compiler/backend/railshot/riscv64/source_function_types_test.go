//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestSourceFunctionTypeDirectoryBorrowsPlainFlatGroups(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}, {SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}}}, {Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.F64}}}}}}}
	want := &m.Types[1].SubTypes[1].Comp
	m.TypeFunc(2) // Build the module's shared type index before measuring the view.
	if got := testing.AllocsPerRun(20, func() {
		d, err := newSourceFunctionTypeDirectory(m)
		if err != nil {
			panic(err)
		}
		actual, ok := d.function(2)
		if !ok || actual != want || len(d.normalized) != 0 {
			panic("flat type was copied or misindexed")
		}
	}); got != 0 {
		t.Fatal("plain borrowed type directory allocated", got)
	}
}

func TestSourceFunctionTypeDirectoryResolvesRecursiveLocalHeap(t *testing.T) {
	reference := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Rec: true, Index: 0}), false))
	m := &wasm.Module{Types: []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}},
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{reference}}}, {Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct}}}},
	}}
	d, err := newSourceFunctionTypeDirectory(m)
	if err != nil {
		t.Fatal(err)
	}
	typ, ok := d.function(1)
	if !ok || typ.Params[0].Ref().Heap().Type() != (wasm.TypeIdx{Index: 1}) {
		t.Fatal("recursive heap did not resolve to its flat group base")
	}
	if m.Types[1].SubTypes[0].Comp.Params[0] != reference {
		t.Fatal("source recursive identity was changed")
	}
	if _, ok := d.function(2); ok {
		t.Fatal("aggregate was classified as callable")
	}
	if _, ok := d.function(^uint32(0)); ok {
		t.Fatal("unknown type index admitted")
	}
	want, ok := m.StructuralTypeKeyChecked(1)
	if !ok {
		t.Fatal("source graph key unavailable")
	}
	if key, ok := d.key(1); !ok || key != want {
		t.Fatal("function key lost source graph identity")
	}
}

func TestPublicGroupedFunctionTypesCompile(t *testing.T) {
	for _, vector := range []bool{false, true} {
		name, carrier, params, body := "scalar", byte(0x7f), byte(0), []byte{0, 0x41, 42, 0x0b}
		if vector {
			name, carrier, params, body = "vector", 0x7b, 1, []byte{0, 0x20, 0, 0x0b}
		}
		t.Run(name, func(t *testing.T) {
			signature := []byte{0x60, params}
			if params != 0 {
				signature = append(signature, carrier)
			}
			signature = append(signature, 1, carrier)
			types := append([]byte{1, 0x4e, 2, 0x50, 0}, signature...)
			types = append(types, 0x4f, 1, 0)
			types = append(types, signature...)
			source := wasmtest.Module(wasmtest.Section(1, types), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal("public grouped wire", err)
			}
			if err := admitWire(source); err == nil {
				t.Fatal("private grouped wire admitted")
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true})
			if err != nil {
				t.Fatal("grouped function compile", err)
			}
			if image.CodeImage != nil {
				image.CodeImage.Close()
			}
			if len(image.Entry) != 1 || len(image.Code) == 0 || len(m.Types) != 1 || len(m.Types[0].SubTypes) != 2 || m.Types[0].SubTypes[0].Final || m.Types[0].SubTypes[1].Supers[0].Index != 0 {
				t.Fatal("source group identity changed")
			}
		})
	}
}

func TestPublicFunctionWireRejectsTruncation(t *testing.T) {
	for _, body := range [][]byte{{1, 0x4e}, {1, 0x4e, 2, 0x60, 0, 0}, {1, 0x50, 1}, {1, 0x60, 1, 0x63}} {
		if err := AdmitProductionSource(wasmtest.Module(wasmtest.Section(1, body))); err == nil {
			t.Fatalf("malformed type bytes admitted: %x", body)
		}
	}
	// An empty recursive group is complete and valid; it declares no types.
	if err := AdmitProductionSource(wasmtest.Module(wasmtest.Section(1, []byte{1, 0x4e, 0}))); err != nil {
		t.Fatal("empty recursive group", err)
	}
}

func TestPublicFlatTypeDirectoryMetadataBudget(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: make([]wasm.SubType, 3)}}}
	for _, limit := range []uint64{11, 12} {
		err := admitPublicDecodedDirectoryBudget(m, CompileOptions{MaxCompiledMetadataBytes: limit})
		if (err == nil) != (limit == 12) {
			t.Fatalf("flat metadata limit=%d: %v", limit, err)
		}
	}
}

func BenchmarkSourceFunctionTypeDirectory(b *testing.B) {
	for _, recursive := range []bool{false, true} {
		name := "borrowed"
		param := wasm.I64
		if recursive {
			name = "recursive"
			param = wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Rec: true}), false))
		}
		m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{param}}}, {Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}}}
		m.TypeFunc(0)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				d, err := newSourceFunctionTypeDirectory(m)
				if err != nil {
					b.Fatal(err)
				}
				if _, ok := d.function(0); !ok {
					b.Fatal("missing function")
				}
			}
		})
	}
}

func TestPublicAggregateTypeDeclarationsHaveNoExecutionState(t *testing.T) {
	source := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x4e, 2, 0x5e, 0x63, 1, 0, 0x5f, 1, 0x63, 0, 1}))
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitPublicTypeDeclarations(m) {
		t.Fatal("pure type graph rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(image.Code) != 0 || len(image.Entry) != 0 {
		t.Fatal("declarations gained executable code")
	}
	m.Start = new(wasm.FuncIdx)
	if admitPublicTypeDeclarations(m) {
		t.Fatal("start execution gained declaration admission")
	}
	m.Start = nil
	m.Globals = []wasm.Global{{}}
	if admitPublicTypeDeclarations(m) {
		t.Fatal("state gained declaration admission")
	}
}

func TestPrimitiveFunctionGraphExcludesActualGCOperations(t *testing.T) {
	for _, test := range []struct {
		body []byte
		want bool
	}{{[]byte{0x41, 0xfb, 0, 0x0b}, true}, {[]byte{0x41, 0, 0xfb, 0x1c, 0xfb, 0x1d, 0x0b}, false}} {
		m, err := wasm.DecodeModule(binaryModule(fixtureFunc{result: 0x7f, body: test.body}))
		if err != nil {
			t.Fatal(err)
		}
		if got := AdmitPrimitiveFunctionGraphModule(m); got != test.want {
			t.Fatalf("graph GC instruction check for %x: %v", test.body, got)
		}
	}
}
