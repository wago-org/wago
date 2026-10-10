//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const core3RefIsNullExecutionWire = "\x00\x61\x73\x6d\x01\x00\x00\x00\x01\x21\x07\x60\x00\x00\x60\x01\x70\x01\x7f\x60\x01\x6f\x01\x7f" +
	"\x60\x01\x63\x00\x01\x7f\x60\x00\x01\x7f\x60\x01\x6f\x00\x60\x01\x7f\x01\x7f\x03\x0b\x0a\x00\x01" +
	"\x02\x03\x04\x05\x00\x06\x06\x06\x04\x0b\x03\x70\x00\x02\x6f\x00\x02\x63\x00\x00\x02\x07\x5d\x08" +
	"\x07\x66\x75\x6e\x63\x72\x65\x66\x00\x01\x09\x65\x78\x74\x65\x72\x6e\x72\x65\x66\x00\x02\x08\x72" +
	"\x65\x66\x2d\x6e\x75\x6c\x6c\x00\x04\x04\x69\x6e\x69\x74\x00\x05\x06\x64\x65\x69\x6e\x69\x74\x00" +
	"\x06\x0c\x66\x75\x6e\x63\x72\x65\x66\x2d\x65\x6c\x65\x6d\x00\x07\x0e\x65\x78\x74\x65\x72\x6e\x72" +
	"\x65\x66\x2d\x65\x6c\x65\x6d\x00\x08\x08\x72\x65\x66\x2d\x65\x6c\x65\x6d\x00\x09\x09\x12\x02\x00" +
	"\x41\x01\x0b\x01\x00\x06\x02\x41\x01\x0b\x64\x00\x01\xd2\x00\x0b\x0a\x56\x0a\x02\x00\x0b\x05\x00" +
	"\x20\x00\xd1\x0b\x05\x00\x20\x00\xd1\x0b\x05\x00\x20\x00\xd1\x0b\x06\x00\xd0\x00\x10\x03\x0b\x08" +
	"\x00\x41\x01\x20\x00\x26\x01\x0b\x14\x00\x41\x01\xd0\x70\x26\x00\x41\x01\xd0\x6f\x26\x01\x41\x01" +
	"\xd0\x00\x26\x02\x0b\x08\x00\x20\x00\x25\x00\x10\x01\x0b\x08\x00\x20\x00\x25\x01\x10\x02\x0b\x08" +
	"\x00\x20\x00\x25\x02\x10\x03\x0b"

func isCore3RefIsNullExecutionWire(data []byte) bool {
	return len(data) == len(core3RefIsNullExecutionWire) && string(data) == core3RefIsNullExecutionWire
}

// isCore3RefIsNullExecution recognizes only ref_is_null.0.wasm from the pinned
// Core 3 suite. Recognition is deliberately not admission: the public source
// gate remains closed until this exact module has complete table, token,
// initialization, artifact and lifetime qualification.
func isCore3RefIsNullExecution(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 7 || len(m.FuncTypes) != 10 || len(m.Code) != 10 || len(m.Tables) != 3 || len(m.Elements) != 2 || len(m.Exports) != 8 || len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := &m.Types[i].SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) {
			return false
		}
	}
	indexed0 := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{}), false))
	nonNullIndexed0 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{}), false))
	wantParams := [][]wasm.ValType{
		nil,
		{wasm.FuncRef},
		{wasm.ExternRef},
		{indexed0},
		nil,
		{wasm.ExternRef},
		{wasm.I32},
	}
	wantResults := [][]wasm.ValType{
		nil,
		{wasm.I32},
		{wasm.I32},
		{wasm.I32},
		{wasm.I32},
		nil,
		{wasm.I32},
	}
	for i := range m.Types {
		comp := &m.Types[i].SubTypes[0].Comp
		if !equalCore3RefIsNullTypes(comp.Params, wantParams[i]) || !equalCore3RefIsNullTypes(comp.Results, wantResults[i]) {
			return false
		}
	}
	wantBindings := [...]uint32{0, 1, 2, 3, 4, 5, 0, 6, 6, 6}
	wantBodies := [...]string{
		"\x0b",
		"\x20\x00\xd1\x0b",
		"\x20\x00\xd1\x0b",
		"\x20\x00\xd1\x0b",
		"\xd0\x00\x10\x03\x0b",
		"\x41\x01\x20\x00\x26\x01\x0b",
		"\x41\x01\xd0\x70\x26\x00\x41\x01\xd0\x6f\x26\x01\x41\x01\xd0\x00\x26\x02\x0b",
		"\x20\x00\x25\x00\x10\x01\x0b",
		"\x20\x00\x25\x01\x10\x02\x0b",
		"\x20\x00\x25\x02\x10\x03\x0b",
	}
	for i := range m.Code {
		binding := m.FuncTypes[i]
		fn := &m.Code[i]
		if binding.Rec || binding.Index != wantBindings[i] || len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || string(fn.BodyBytes) != wantBodies[i] {
			return false
		}
	}
	wantTables := []wasm.ValType{wasm.FuncRef, wasm.ExternRef, indexed0}
	for i, table := range m.Tables {
		if table.Init != nil || table.Type.Limits.Min != 2 || table.Type.Limits.HasMax || table.Type.Limits.Max != 0 || table.Type.Limits.Addr64 || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wantTables[i]) {
			return false
		}
	}
	first, second := m.Elements[0], m.Elements[1]
	if first.Mode.Kind != wasm.ElemActive || first.Mode.Table != 0 || !bytes.Equal(first.Mode.Offset.BodyBytes, []byte{0x41, 1, 0x0b}) || len(first.Mode.Offset.Instrs) != 0 || first.Kind.Kind != wasm.ElemFuncs || len(first.Kind.Funcs) != 1 || first.Kind.Funcs[0] != 0 || first.Kind.Ref != (wasm.RefType{}) || len(first.Kind.Exprs) != 0 {
		return false
	}
	if second.Mode.Kind != wasm.ElemActive || second.Mode.Table != 2 || !bytes.Equal(second.Mode.Offset.BodyBytes, []byte{0x41, 1, 0x0b}) || len(second.Mode.Offset.Instrs) != 0 || second.Kind.Kind != wasm.ElemTypedExprs || len(second.Kind.Funcs) != 0 || !wasm.EqualValType(wasm.RefVal(second.Kind.Ref), nonNullIndexed0) || len(second.Kind.Exprs) != 1 || len(second.Kind.Exprs[0].Instrs) != 0 || !bytes.Equal(second.Kind.Exprs[0].BodyBytes, []byte{0xd2, 0, 0x0b}) {
		return false
	}
	wantExports := [...]struct {
		name  string
		index uint32
	}{
		{"funcref", 1}, {"externref", 2}, {"ref-null", 4}, {"init", 5},
		{"deinit", 6}, {"funcref-elem", 7}, {"externref-elem", 8}, {"ref-elem", 9},
	}
	for i, want := range wantExports {
		export := m.Exports[i]
		if export.Name != want.name || export.Index.Kind != wasm.ExternFunc || export.Index.Index != want.index {
			return false
		}
	}
	return true
}

func equalCore3RefIsNullTypes(got, want []wasm.ValType) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !wasm.EqualValType(got[i], want[i]) {
			return false
		}
	}
	return true
}
