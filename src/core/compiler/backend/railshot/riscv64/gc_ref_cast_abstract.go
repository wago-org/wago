//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCRefCastAbstractHex = "0061736d01000000019180808000056000005f005e780060016f0060017f000388808080000700030404040404048480808000016e000a07de808080000604696e69740001117265665f636173745f6e6f6e5f6e756c6c00020d7265665f636173745f6e756c6c00030c7265665f636173745f69333100040f7265665f636173745f73747275637400050e7265665f636173745f6172726179000609858080800001030001000ad081808000078280808000000bba80808000004100d06e260041014107fb1c26004102fb0101260041034100fb0702260041042000fb1a26004105d06c26004106d06b26004107d07126000b90808080000020002500d41a20002500fb176e1a0baa808080000020002500fb176e1a20002500fb176b1a20002500fb176a1a20002500fb176c1a20002500fb17711a0b92808080000020002500fb166c1a20002500fb176c1a0b92808080000020002500fb166b1a20002500fb176b1a0b92808080000020002500fb166a1a20002500fb176a1a0b"

var rv64GCRefCastAbstractSourceDigest = [sha256.Size]byte{0xc8, 0x55, 0x56, 0x08, 0x9b, 0xf1, 0xa3, 0x9c, 0xb3, 0x08, 0x2f, 0x7d, 0xe9, 0x16, 0xc0, 0x0e, 0xaa, 0x24, 0x82, 0x25, 0x3c, 0xf1, 0x26, 0xd8, 0xa9, 0xfc, 0x09, 0xab, 0x97, 0x0e, 0xed, 0x4b}

func admitRV64GCRefCastAbstractWire(data []byte) bool {
	if len(data) != 380 || sha256.Sum256(data) != rv64GCRefCastAbstractSourceDigest || !admitExactProductWireShape(data, []byte{1, 3, 4, 7, 9, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCRefCastAbstract(m)
}

// AdmitGCRefCastAbstractSource is the exact-wire witness for the official
// bounded abstract anyref ref.cast checkpoint.
func AdmitGCRefCastAbstractSource(data []byte) bool { return admitRV64GCRefCastAbstractWire(data) }

func admitRV64GCRefCastAbstract(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 5 || len(m.FuncTypes) != 7 || len(m.Code) != 7 || len(m.Tables) != 1 || len(m.Exports) != 6 || len(m.Elements) != 1 ||
		len(m.Imports) != 0 || len(m.Globals) != 0 || len(m.Memories) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil ||
		len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	if !i31CoreFunctionType(&m.Types[0], nil, nil) || !rv64ExactEmptyStructType(&m.Types[1]) || !rv64ExactPackedI8ArrayType(&m.Types[2], false) ||
		!i31CoreFunctionType(&m.Types[3], []wasm.ValType{wasm.ExternRef}, nil) || !i31CoreFunctionType(&m.Types[4], []wasm.ValType{wasm.I32}, nil) {
		return false
	}
	table := m.Tables[0]
	if table.Init != nil || table.Type.Limits.Addr64 || table.Type.Limits.HasMax || table.Type.Limits.Min != 10 || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.AnyRef) {
		return false
	}
	elem := &m.Elements[0]
	if elem.Mode.Kind != wasm.ElemDeclarative || elem.Mode.Table != 0 || len(elem.Mode.Offset.Instrs) != 0 || len(elem.Mode.Offset.BodyBytes) != 0 || elem.Kind.Kind != wasm.ElemFuncs || len(elem.Kind.Funcs) != 1 || elem.Kind.Funcs[0] != 0 || len(elem.Kind.Exprs) != 0 {
		return false
	}
	wantTypeIndexes := []uint32{0, 3, 4, 4, 4, 4, 4}
	wantBodies := [][]byte{
		{0x0b},
		{0x41, 0x00, 0xd0, 0x6e, 0x26, 0x00, 0x41, 0x01, 0x41, 0x07, 0xfb, 0x1c, 0x26, 0x00, 0x41, 0x02, 0xfb, 0x01, 0x01, 0x26, 0x00, 0x41, 0x03, 0x41, 0x00, 0xfb, 0x07, 0x02, 0x26, 0x00, 0x41, 0x04, 0x20, 0x00, 0xfb, 0x1a, 0x26, 0x00, 0x41, 0x05, 0xd0, 0x6c, 0x26, 0x00, 0x41, 0x06, 0xd0, 0x6b, 0x26, 0x00, 0x41, 0x07, 0xd0, 0x71, 0x26, 0x00, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xd4, 0x1a, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x17, 0x6e, 0x1a, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x17, 0x6e, 0x1a, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x17, 0x6b, 0x1a, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x17, 0x6a, 0x1a, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x17, 0x6c, 0x1a, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x17, 0x71, 0x1a, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x16, 0x6c, 0x1a, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x17, 0x6c, 0x1a, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x16, 0x6b, 0x1a, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x17, 0x6b, 0x1a, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x16, 0x6a, 0x1a, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x17, 0x6a, 0x1a, 0x0b},
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantTypeIndexes[i]}) || len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []string{"init", "ref_cast_non_null", "ref_cast_null", "ref_cast_i31", "ref_cast_struct", "ref_cast_array"}
	for i, name := range wantExports {
		got := m.Exports[i]
		if got.Name != name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != uint32(i+1) {
			return false
		}
	}
	return true
}
