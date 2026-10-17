//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCExternHex = "0061736d0100000001a180808000086000005f005e780060016f0060016f016e60016e016f60017f016f60017f016e03878080800006000304050607048480808000016e000a068f80808000026f00d06efb1b0b6e00d06ffb1a0b07c5808080000504696e697400010b696e7465726e616c697a6500020b65787465726e616c697a6500030d65787465726e616c697a652d6900040e65787465726e616c697a652d6969000509858080800001030001000ae780808000068280808000000ba880808000004100d06e260041014107fb1c26004102fb0101260041034100fb0702260041042000fb1a26000b8680808000002000fb1a0b8680808000002000fb1b0b88808080000020002500fb1b0b8a808080000020002500fb1bfb1a0b"

func admitRV64GCExternWire(data []byte) bool {
	if len(data) != 286 || !admitExactProductWireShape(data, []byte{1, 3, 4, 6, 7, 9, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCExtern(m)
}

// AdmitGCExternSource is the exact-wire witness for the official bounded
// anyref/externref conversion and rooted object-table checkpoint.
func AdmitGCExternSource(data []byte) bool { return admitRV64GCExternWire(data) }

func admitRV64GCExtern(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 8 || len(m.FuncTypes) != 6 || len(m.Code) != 6 || len(m.Globals) != 2 || len(m.Tables) != 1 || len(m.Exports) != 5 || len(m.Elements) != 1 ||
		len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil ||
		len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	if !i31CoreFunctionType(&m.Types[0], nil, nil) || !rv64ExactEmptyStructType(&m.Types[1]) || !rv64ExactPackedI8ArrayType(&m.Types[2], false) ||
		!i31CoreFunctionType(&m.Types[3], []wasm.ValType{wasm.ExternRef}, nil) ||
		!i31CoreFunctionType(&m.Types[4], []wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.AnyRef}) ||
		!i31CoreFunctionType(&m.Types[5], []wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.ExternRef}) ||
		!i31CoreFunctionType(&m.Types[6], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.ExternRef}) ||
		!i31CoreFunctionType(&m.Types[7], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.AnyRef}) {
		return false
	}
	wantGlobals := []struct {
		typ  wasm.ValType
		body []byte
	}{
		{wasm.ExternRef, []byte{0xd0, 0x6e, 0xfb, 0x1b, 0x0b}},
		{wasm.AnyRef, []byte{0xd0, 0x6f, 0xfb, 0x1a, 0x0b}},
	}
	for i, want := range wantGlobals {
		got := &m.Globals[i]
		if got.Type.Mutable || !wasm.EqualValType(got.Type.Type, want.typ) || len(got.Init.Instrs) != 0 || !bytes.Equal(got.Init.BodyBytes, want.body) {
			return false
		}
	}
	table := m.Tables[0]
	if table.Init != nil || table.Type.Limits.Addr64 || table.Type.Limits.HasMax || table.Type.Limits.Min != 10 || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.AnyRef) {
		return false
	}
	elem := &m.Elements[0]
	if elem.Mode.Kind != wasm.ElemDeclarative || elem.Mode.Table != 0 || len(elem.Mode.Offset.Instrs) != 0 || len(elem.Mode.Offset.BodyBytes) != 0 || elem.Kind.Kind != wasm.ElemFuncs || len(elem.Kind.Funcs) != 1 || elem.Kind.Funcs[0] != 0 || len(elem.Kind.Exprs) != 0 {
		return false
	}
	wantTypeIndexes := []uint32{0, 3, 4, 5, 6, 7}
	wantBodies := [][]byte{
		{0x0b},
		{0x41, 0x00, 0xd0, 0x6e, 0x26, 0x00, 0x41, 0x01, 0x41, 0x07, 0xfb, 0x1c, 0x26, 0x00, 0x41, 0x02, 0xfb, 0x01, 0x01, 0x26, 0x00, 0x41, 0x03, 0x41, 0x00, 0xfb, 0x07, 0x02, 0x26, 0x00, 0x41, 0x04, 0x20, 0x00, 0xfb, 0x1a, 0x26, 0x00, 0x0b},
		{0x20, 0x00, 0xfb, 0x1a, 0x0b},
		{0x20, 0x00, 0xfb, 0x1b, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x1b, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x1b, 0xfb, 0x1a, 0x0b},
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantTypeIndexes[i]}) || len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []string{"init", "internalize", "externalize", "externalize-i", "externalize-ii"}
	for i, name := range wantExports {
		got := m.Exports[i]
		if got.Name != name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != uint32(i+1) {
			return false
		}
	}
	return true
}

func rv64ExactEmptyStructType(rec *wasm.RecType) bool {
	if rec == nil || len(rec.SubTypes) != 1 {
		return false
	}
	sub := &rec.SubTypes[0]
	return sub.Final && !sub.HasPrefix && len(sub.Supers) == 0 && !sub.Metadata.Describes.Present() && !sub.Metadata.Descriptor.Present() && sub.Comp.Kind == wasm.CompStruct && len(sub.Comp.Fields) == 0
}
