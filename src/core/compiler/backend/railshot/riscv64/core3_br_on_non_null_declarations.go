//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var core3BrOnNonNullDeclarationsWire = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x11, 0x04, 0x60, 0x00, 0x00, 0x60, 0x01,
	0x63, 0x00, 0x00, 0x60, 0x01, 0x70, 0x00, 0x60,
	0x01, 0x6f, 0x00, 0x03, 0x04, 0x03, 0x01, 0x02,
	0x03, 0x0a, 0x28, 0x03, 0x0c, 0x00, 0x02, 0x64,
	0x00, 0x20, 0x00, 0xd6, 0x00, 0x00, 0x0b, 0x1a,
	0x0b, 0x0c, 0x00, 0x02, 0x64, 0x70, 0x20, 0x00,
	0xd6, 0x00, 0x00, 0x0b, 0x1a, 0x0b, 0x0c, 0x00,
	0x02, 0x64, 0x6f, 0x20, 0x00, 0xd6, 0x00, 0x00,
	0x0b, 0x1a, 0x0b,
}

func admitCore3BrOnNonNullDeclarationsWire(data []byte) bool {
	return bytes.Equal(data, core3BrOnNonNullDeclarationsWire)
}

// admitCore3BrOnNonNullDeclarations recognizes only br_on_non_null.1.wasm
// from the pinned Release 3 fixture. Its three private functions exercise
// non-null indexed, func and extern block results, but no function or value is
// nameable through an export, start, element, ref.func, call or resource.
func admitCore3BrOnNonNullDeclarations(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 4 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Imports) != 0 || len(m.Exports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.NameSec != nil || len(m.RawNameSecPayload) != 0 || m.BranchHints != nil || m.UsesCompactImports {
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
	wantParams := []wasm.ValType{
		wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{}), false)),
		wasm.FuncRef,
		wasm.ExternRef,
	}
	for i, want := range wantParams {
		params := m.Types[i+1].SubTypes[0].Comp.Params
		if len(params) != 1 || !wasm.EqualValType(params[0], want) {
			return false
		}
	}
	wantBodies := [][]byte{
		{0x02, 0x64, 0x00, 0x20, 0x00, 0xd6, 0x00, 0x00, 0x0b, 0x1a, 0x0b},
		{0x02, 0x64, 0x70, 0x20, 0x00, 0xd6, 0x00, 0x00, 0x0b, 0x1a, 0x0b},
		{0x02, 0x64, 0x6f, 0x20, 0x00, 0xd6, 0x00, 0x00, 0x0b, 0x1a, 0x0b},
	}
	for i := range m.Code {
		index := m.FuncTypes[i]
		fn := &m.Code[i]
		if index.Rec || index.Index != uint32(i+1) || len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	return true
}
