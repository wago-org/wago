//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayLocalWire(data []byte) bool {
	if !admitExactProductWireShape(data, []byte{1, 3, 7, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCArrayLocal(m)
}

// AdmitGCArrayLocalSource is the wire-provenance half of the exact numeric
// array.new_default/get/set/len checkpoint.
func AdmitGCArrayLocalSource(data []byte) bool { return admitRV64GCArrayLocalWire(data) }

func admitRV64GCArrayLocal(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 4 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Exports) != 3 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
	}
	array := &m.Types[0].SubTypes[0]
	if !array.Final || array.HasPrefix || len(array.Supers) != 0 || array.Metadata.Describes.Present() || array.Metadata.Descriptor.Present() ||
		array.Comp.Kind != wasm.CompArray || array.Comp.Array != wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Var) {
		return false
	}
	wantFunctions := []struct {
		params, results []wasm.ValType
		typeIndex       uint32
		body            []byte
	}{
		{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, 1, []byte{0x20, 0x00, 0xfb, 0x07, 0x00, 0x20, 0x01, 0xfb, 0x0b, 0x00, 0x0b}},
		{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, 2, []byte{0x20, 0x00, 0xfb, 0x07, 0x00, 0x21, 0x03, 0x20, 0x03, 0x20, 0x01, 0x20, 0x02, 0xfb, 0x0e, 0x00, 0x20, 0x03, 0x20, 0x01, 0xfb, 0x0b, 0x00, 0x0b}},
		{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, 3, []byte{0x20, 0x00, 0xfb, 0x07, 0x00, 0xfb, 0x0f, 0x0b}},
	}
	for i, want := range wantFunctions {
		if !i31CoreFunctionType(&m.Types[i+1], want.params, want.results) || m.FuncTypes[i] != (wasm.TypeIdx{Index: want.typeIndex}) {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, want.body) {
			return false
		}
		if i != 1 && (len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1) {
			return false
		}
	}
	ref := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	if len(m.Code[1].Locals.Runs) != 1 || m.Code[1].Locals.Runs[0] != (wasm.LocalRun{Count: 1, Type: ref}) || m.Code[1].LocalDeclBytes != 4 {
		return false
	}
	wantExports := []struct {
		name  string
		index uint32
	}{{"get", 0}, {"set_get", 1}, {"len", 2}}
	for i, want := range wantExports {
		got := m.Exports[i]
		if got.Name != want.name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != want.index {
			return false
		}
	}
	return true
}
