//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var core3RefDeclarationsWire = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x1b, 0x02, 0x60, 0x00, 0x00, 0x60, 0x0c,
	0x70, 0x6f, 0x64, 0x70, 0x64, 0x6f, 0x64, 0x00,
	0x64, 0x00, 0x64, 0x00, 0x64, 0x00, 0x70, 0x6f,
	0x63, 0x00, 0x63, 0x00, 0x00, 0x03, 0x02, 0x01,
	0x01, 0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b,
}

func admitCore3RefDeclarationsWire(data []byte) bool {
	return bytes.Equal(data, core3RefDeclarationsWire)
}

// admitCore3RefDeclarations recognizes only ref.0.wasm from the pinned
// Release 3 fixture. Its single private function has the complete nullable and
// non-null abstract/indexed reference parameter matrix, but no function or
// value is nameable through an export, start, element, ref.func, call or
// resource.
func admitCore3RefDeclarations(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 2 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Imports) != 0 || len(m.Exports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := &m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) || len(sub.Comp.Results) != 0 {
			return false
		}
	}
	if len(m.Types[0].SubTypes[0].Comp.Params) != 0 {
		return false
	}
	indexed := wasm.IndexedHeap(wasm.TypeIdx{})
	wantParams := []wasm.ValType{
		wasm.FuncRef,
		wasm.ExternRef,
		wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false)),
		wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapExtern), false)),
		wasm.RefVal(wasm.Ref(false, indexed, false)),
		wasm.RefVal(wasm.Ref(false, indexed, false)),
		wasm.RefVal(wasm.Ref(false, indexed, false)),
		wasm.RefVal(wasm.Ref(false, indexed, false)),
		wasm.FuncRef,
		wasm.ExternRef,
		wasm.RefVal(wasm.Ref(true, indexed, false)),
		wasm.RefVal(wasm.Ref(true, indexed, false)),
	}
	params := m.Types[1].SubTypes[0].Comp.Params
	if len(params) != len(wantParams) {
		return false
	}
	for i := range wantParams {
		if !wasm.EqualValType(params[i], wantParams[i]) {
			return false
		}
	}
	index := m.FuncTypes[0]
	fn := &m.Code[0]
	return !index.Rec && index.Index == 1 && len(fn.Locals.Runs) == 0 && fn.LocalDeclBytes == 1 && len(fn.Body.Instrs) == 0 && len(fn.Body.BodyBytes) == 0 && bytes.Equal(fn.BodyBytes, []byte{0x0b})
}
