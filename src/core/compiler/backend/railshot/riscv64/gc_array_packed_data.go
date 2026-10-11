//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayPackedDataWire(data []byte) bool {
	if !admitExactProductWireShape(data, []byte{1, 3, 7, 12, 10, 11}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCArrayPackedData(m)
}

func AdmitGCArrayPackedDataSource(data []byte) bool { return admitRV64GCArrayPackedDataWire(data) }

func rv64ExactPackedI8ArrayType(group *wasm.RecType, mutable bool) bool {
	if group == nil || len(group.SubTypes) != 1 {
		return false
	}
	wantMut := wasm.Const
	if mutable {
		wantMut = wasm.Var
	}
	sub := &group.SubTypes[0]
	return sub.Final && !sub.HasPrefix && len(sub.Supers) == 0 && !sub.Metadata.Describes.Present() && !sub.Metadata.Descriptor.Present() &&
		sub.Comp.Kind == wasm.CompArray && sub.Comp.Array == wasm.NewFieldType(wasm.StoragePacked(wasm.PackI8), wantMut)
}

func admitRV64GCArrayPackedData(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 10 || len(m.FuncTypes) != 11 || len(m.Code) != 11 || len(m.Exports) != 7 || len(m.Data) != 1 || m.DataCount == nil || *m.DataCount != 1 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports ||
		!rv64ExactPackedI8ArrayType(&m.Types[0], false) || !rv64ExactPackedI8ArrayType(&m.Types[1], true) {
		return false
	}
	if m.Data[0].Mode.Kind != wasm.DataPassive || m.Data[0].Mode.Mem != 0 ||
		len(m.Data[0].Mode.Offset.BodyBytes) != 0 || len(m.Data[0].Mode.Offset.Instrs) != 0 ||
		!bytes.Equal(m.Data[0].Init, []byte{0, 1, 2, 0xff, 4}) {
		return false
	}
	ref0 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	ref1 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
	arrayRef := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapArray), false))
	types := []struct{ params, results []wasm.ValType }{
		{nil, []wasm.ValType{ref0}},
		{[]wasm.ValType{wasm.I32, ref0}, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{wasm.I32, ref1, wasm.I32}, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}},
		{[]wasm.ValType{arrayRef}, []wasm.ValType{wasm.I32}},
		{nil, []wasm.ValType{wasm.I32}},
		{nil, nil},
	}
	for i, want := range types {
		if !i31CoreFunctionType(&m.Types[i+2], want.params, want.results) {
			return false
		}
	}
	wantFuncTypes := []uint32{2, 2, 3, 4, 3, 4, 5, 6, 7, 8, 9}
	wantBodies := [][]byte{
		{0x41, 1, 0x41, 3, 0xfb, 0x09, 0, 0, 0x0b},
		{0x41, 0x80, 0x80, 0x80, 0x80, 0x78, 0x41, 0x80, 0x80, 0x80, 0x80, 0x78, 0xfb, 0x09, 0, 0, 0x0b},
		{0x20, 1, 0x20, 0, 0xfb, 0x0d, 0, 0x0b},
		{0x20, 0, 0x10, 0, 0x10, 2, 0x0b},
		{0x20, 1, 0x20, 0, 0xfb, 0x0c, 0, 0x0b},
		{0x20, 0, 0x10, 0, 0x10, 4, 0x0b},
		{0x20, 1, 0x20, 0, 0x20, 2, 0xfb, 0x0e, 1, 0x20, 1, 0x20, 0, 0xfb, 0x0d, 1, 0x0b},
		{0x20, 0, 0x41, 1, 0x41, 3, 0xfb, 0x09, 1, 0, 0x20, 1, 0x10, 6, 0x0b},
		{0x20, 0, 0xfb, 0x0f, 0x0b},
		{0x10, 0, 0x10, 8, 0x0b},
		{0xfc, 0x09, 0, 0x0b},
	}
	for i := range m.Code {
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantFuncTypes[i]}) {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []struct {
		name  string
		index uint32
	}{{"new", 0}, {"new-overflow", 1}, {"get_u", 3}, {"get_s", 5}, {"set_get", 7}, {"len", 9}, {"drop_segs", 10}}
	for i, want := range wantExports {
		got := m.Exports[i]
		if got.Name != want.name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != want.index {
			return false
		}
	}
	return true
}
