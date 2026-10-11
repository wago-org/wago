//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayFixedOfficialWire(data []byte) bool {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) {
		return false
	}
	// Preserve only representation that survives the decoded whole-module
	// proof below. Present empty sections and explicit recursive wrappers are
	// semantically inert after decoding, but are outside this first checkpoint.
	wantIDs := [...]byte{1, 3, 6, 7, 10}
	wantCounts := [...]uint32{9, 7, 1, 4, 7}
	r := wasm.ReaderFrom(data[8:])
	sectionIndex := 0
	for r.HasNext() {
		if sectionIndex == len(wantIDs) {
			return false
		}
		id, err := r.Byte()
		if err != nil || id != wantIDs[sectionIndex] {
			return false
		}
		n, err := r.U32()
		if err != nil || uint64(n) > uint64(r.BytesLeft()) {
			return false
		}
		start := r.Offset()
		end := start + int(n)
		section := wasm.ReaderFrom(data[8+start : 8+end])
		count, err := section.U32()
		if err != nil || count != wantCounts[sectionIndex] {
			return false
		}
		if id == 1 {
			for _, b := range data[8+section.Offset()+start : 8+end] {
				if b == 0x4e { // explicit rec group, flattened by public decoding
					return false
				}
			}
		}
		if err = r.JumpTo(end); err != nil {
			return false
		}
		sectionIndex++
	}
	return sectionIndex == len(wantIDs)
}

func AdmitGCArrayFixedOfficialSource(data []byte) bool {
	return admitRV64GCArrayFixedOfficialWire(data)
}

func rv64ExactArrayType(group *wasm.RecType, mutable bool) bool {
	if group == nil || len(group.SubTypes) != 1 {
		return false
	}
	wantMut := wasm.Const
	if mutable {
		wantMut = wasm.Var
	}
	sub := &group.SubTypes[0]
	return sub.Final && !sub.HasPrefix && len(sub.Supers) == 0 && !sub.Metadata.Describes.Present() && !sub.Metadata.Descriptor.Present() &&
		sub.Comp.Kind == wasm.CompArray && sub.Comp.Array == wasm.NewFieldType(wasm.StorageVal(wasm.F32), wantMut)
}

func admitRV64GCArrayFixedOfficial(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 9 || len(m.FuncTypes) != 7 || len(m.Code) != 7 || len(m.Globals) != 1 || len(m.Exports) != 4 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports || !rv64ExactArrayType(&m.Types[0], false) || !rv64ExactArrayType(&m.Types[1], true) {
		return false
	}
	ref0 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	ref1 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
	arrayRef := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapArray), false))
	types := []struct{ params, results []wasm.ValType }{
		{nil, []wasm.ValType{ref0}},
		{[]wasm.ValType{wasm.I32, ref0}, []wasm.ValType{wasm.F32}},
		{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F32}},
		{[]wasm.ValType{wasm.I32, ref1, wasm.F32}, []wasm.ValType{wasm.F32}},
		{[]wasm.ValType{wasm.I32, wasm.F32}, []wasm.ValType{wasm.F32}},
		{[]wasm.ValType{arrayRef}, []wasm.ValType{wasm.I32}},
		{nil, []wasm.ValType{wasm.I32}},
	}
	wantBodies := [][]byte{
		{0x43, 0, 0, 0x80, 0x3f, 0x43, 0, 0, 0, 0x40, 0xfb, 0x08, 0, 2, 0x0b},
		{0x20, 1, 0x20, 0, 0xfb, 0x0b, 0, 0x0b},
		{0x20, 0, 0x10, 0, 0x10, 1, 0x0b},
		{0x20, 1, 0x20, 0, 0x20, 2, 0xfb, 0x0e, 1, 0x20, 1, 0x20, 0, 0xfb, 0x0b, 1, 0x0b},
		{0x20, 0, 0x43, 0, 0, 0x80, 0x3f, 0x43, 0, 0, 0, 0x40, 0x43, 0, 0, 0x40, 0x40, 0xfb, 0x08, 1, 3, 0x20, 1, 0x10, 3, 0x0b},
		{0x20, 0, 0xfb, 0x0f, 0x0b},
		{0x10, 0, 0x10, 5, 0x0b},
	}
	for i := range types {
		if !i31CoreFunctionType(&m.Types[i+2], types[i].params, types[i].results) || m.FuncTypes[i] != (wasm.TypeIdx{Index: uint32(i + 2)}) {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	g := m.Globals[0]
	if !wasm.EqualValType(g.Type.Type, ref0) || g.Type.Mutable || len(g.Init.Instrs) != 0 || !bytes.Equal(g.Init.BodyBytes, wantBodies[0]) {
		return false
	}
	wantExports := []struct {
		name  string
		index uint32
	}{{"new", 0}, {"get", 2}, {"set_get", 4}, {"len", 6}}
	for i, want := range wantExports {
		got := m.Exports[i]
		if got.Name != want.name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != want.index {
			return false
		}
	}
	return true
}
