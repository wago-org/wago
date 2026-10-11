//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayDefaultOfficialWire(data []byte) bool {
	if !admitExactProductWireShape(data, []byte{1, 3, 6, 7, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCArrayDefaultOfficial(m)
}

func AdmitGCArrayDefaultOfficialSource(data []byte) bool {
	return admitRV64GCArrayDefaultOfficialWire(data)
}

func admitRV64GCArrayOfficial(m *wasm.Module) bool {
	return admitRV64GCArrayFixedOfficial(m) || admitRV64GCArrayDefaultOfficial(m)
}

func admitRV64GCArrayDefaultOfficial(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 9 || len(m.FuncTypes) != 7 || len(m.Code) != 7 || len(m.Globals) != 2 || len(m.Exports) != 4 ||
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
		{0x41, 3, 0xfb, 0x07, 0, 0x0b},
		{0x20, 1, 0x20, 0, 0xfb, 0x0b, 0, 0x0b},
		{0x20, 0, 0x10, 0, 0x10, 1, 0x0b},
		{0x20, 1, 0x20, 0, 0x20, 2, 0xfb, 0x0e, 1, 0x20, 1, 0x20, 0, 0xfb, 0x0b, 1, 0x0b},
		{0x20, 0, 0x41, 3, 0xfb, 0x07, 1, 0x20, 1, 0x10, 3, 0x0b},
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
	wantGlobals := [][]byte{
		{0x43, 0, 0, 0x80, 0x3f, 0x41, 3, 0xfb, 0x06, 0, 0x0b},
		wantBodies[0],
	}
	for i := range m.Globals {
		g := m.Globals[i]
		if !wasm.EqualValType(g.Type.Type, ref0) || g.Type.Mutable || len(g.Init.Instrs) != 0 || !bytes.Equal(g.Init.BodyBytes, wantGlobals[i]) {
			return false
		}
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
