//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCArrayReferenceHex = "0061736d0100000001cd808080000f5e78005e6400005e6400015e6300005e6e0160000164016000016403600001640460037f7f6401017f60027f7f017f60047f7f64027f017f60037f7f7f017f6001646a017f6000017f600000038c808080000b0505060708090a0b0c0d0e07b88080800006036e657700000c6e65772d6f766572666c6f770001036765740005077365745f6765740007036c656e00090964726f705f73656773000a099680808000010564000241074103fb06000b41014102fb0800020b0abf818080000b8a808080000041004102fb0a01000b928080800000418080808078418080808078fb0a01000b8a808080000041004102fb0a03000b8a808080000041004102fb0a04000b8e808080000020022000fb0b012001fb0d000b8a808080000020002001100010040b9c80808000002002200020022003fb0b02fb0e0220022000fb0b022001fb0d000b9280808000002000200141004102fb0a0200200210060b8680808000002000fb0f0b868080800000100010080b858080800000fc0d000b"

func admitRV64GCArrayReferenceWire(data []byte) bool {
	if !admitExactProductWireShape(data, []byte{1, 3, 7, 9, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCArrayReference(m)
}

func AdmitGCArrayReferenceSource(data []byte) bool { return admitRV64GCArrayReferenceWire(data) }

func rv64ExactReferenceArrayType(group *wasm.RecType, value wasm.ValType, mutable bool) bool {
	if group == nil || len(group.SubTypes) != 1 {
		return false
	}
	wantMut := wasm.Const
	if mutable {
		wantMut = wasm.Var
	}
	sub := &group.SubTypes[0]
	return sub.Final && !sub.HasPrefix && len(sub.Supers) == 0 && !sub.Metadata.Describes.Present() && !sub.Metadata.Descriptor.Present() &&
		sub.Comp.Kind == wasm.CompArray && sub.Comp.Array == wasm.NewFieldType(wasm.StorageVal(value), wantMut)
}

func admitRV64GCArrayReference(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 15 || len(m.FuncTypes) != 11 || len(m.Code) != 11 || len(m.Exports) != 6 || len(m.Elements) != 1 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	ref0 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	ref0Null := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	if !rv64ExactPackedI8ArrayType(&m.Types[0], false) ||
		!rv64ExactReferenceArrayType(&m.Types[1], ref0, false) ||
		!rv64ExactReferenceArrayType(&m.Types[2], ref0, true) ||
		!rv64ExactReferenceArrayType(&m.Types[3], ref0Null, false) ||
		!rv64ExactReferenceArrayType(&m.Types[4], wasm.AnyRef, true) {
		return false
	}
	ref1 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
	ref2 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 2}), false))
	ref3 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 3}), false))
	ref4 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 4}), false))
	arrayRef := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapArray), false))
	types := []struct{ params, results []wasm.ValType }{
		{nil, []wasm.ValType{ref1}},
		{nil, []wasm.ValType{ref3}},
		{nil, []wasm.ValType{ref4}},
		{[]wasm.ValType{wasm.I32, wasm.I32, ref1}, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{wasm.I32, wasm.I32, ref2, wasm.I32}, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{arrayRef}, []wasm.ValType{wasm.I32}},
		{nil, []wasm.ValType{wasm.I32}},
		{nil, nil},
	}
	wantFuncTypes := []uint32{5, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
	wantBodies := [][]byte{
		{0x41, 0, 0x41, 2, 0xfb, 0x0a, 1, 0, 0x0b},
		{0x41, 0x80, 0x80, 0x80, 0x80, 0x78, 0x41, 0x80, 0x80, 0x80, 0x80, 0x78, 0xfb, 0x0a, 1, 0, 0x0b},
		{0x41, 0, 0x41, 2, 0xfb, 0x0a, 3, 0, 0x0b},
		{0x41, 0, 0x41, 2, 0xfb, 0x0a, 4, 0, 0x0b},
		{0x20, 2, 0x20, 0, 0xfb, 0x0b, 1, 0x20, 1, 0xfb, 0x0d, 0, 0x0b},
		{0x20, 0, 0x20, 1, 0x10, 0, 0x10, 4, 0x0b},
		{0x20, 2, 0x20, 0, 0x20, 2, 0x20, 3, 0xfb, 0x0b, 2, 0xfb, 0x0e, 2, 0x20, 2, 0x20, 0, 0xfb, 0x0b, 2, 0x20, 1, 0xfb, 0x0d, 0, 0x0b},
		{0x20, 0, 0x20, 1, 0x41, 0, 0x41, 2, 0xfb, 0x0a, 2, 0, 0x20, 2, 0x10, 6, 0x0b},
		{0x20, 0, 0xfb, 0x0f, 0x0b},
		{0x10, 0, 0x10, 8, 0x0b},
		{0xfc, 0x0d, 0, 0x0b},
	}
	for i, want := range types {
		if !i31CoreFunctionType(&m.Types[i+5], want.params, want.results) || m.FuncTypes[i] != (wasm.TypeIdx{Index: wantFuncTypes[i]}) {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	e := m.Elements[0]
	if e.Mode.Kind != wasm.ElemPassive || e.Mode.Table != 0 || len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 ||
		e.Kind.Kind != wasm.ElemTypedExprs || len(e.Kind.Funcs) != 0 || e.Kind.Ref != wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false) || len(e.Kind.Exprs) != 2 {
		return false
	}
	wantExprs := [][]byte{{0x41, 7, 0x41, 3, 0xfb, 6, 0, 0x0b}, {0x41, 1, 0x41, 2, 0xfb, 8, 0, 2, 0x0b}}
	for i := range e.Kind.Exprs {
		if len(e.Kind.Exprs[i].Instrs) != 0 || !bytes.Equal(e.Kind.Exprs[i].BodyBytes, wantExprs[i]) {
			return false
		}
	}
	wantExports := []struct {
		name  string
		index uint32
	}{{"new", 0}, {"new-overflow", 1}, {"get", 5}, {"set_get", 7}, {"len", 9}, {"drop_segs", 10}}
	for i, want := range wantExports {
		got := m.Exports[i]
		if got.Name != want.name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != want.index {
			return false
		}
	}
	return true
}
