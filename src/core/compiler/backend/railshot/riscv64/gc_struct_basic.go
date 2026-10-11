//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCStructBasicHex = "0061736d0100000001a380808000065f037d007d017d006000016e60016400017d6000017d600264007d017d60017d017d038e808080000d01020302030203020304050405069e8080800002640000430000803f43000000404300004040fb00000b640000fb01000b07cb8080800007036e65770000076765745f305f300002096765745f7665635f300004076765745f305f790006096765745f7665635f790008097365745f6765745f79000a097365745f6765745f31000c0ab5818080000d858080800000fb01000b8880808000002000fb0200000b878080800000fb010010010b8880808000002000fb0200000b878080800000fb010010030b8880808000002000fb0200010b878080800000fb010010050b8880808000002000fb0200010b878080800000fb010010070b90808080000020002001fb0500012000fb0200010b898080800000fb0100200010090b90808080000020002001fb0500012000fb0200010b898080800000fb01002000100b0b"

var rv64GCStructBasicSourceDigest = [sha256.Size]byte{0xa4, 0x69, 0xba, 0x81, 0xd1, 0x4d, 0xdf, 0x21, 0x10, 0x0f, 0x10, 0x0c, 0xb6, 0x7d, 0x75, 0x94, 0x2b, 0xc0, 0x04, 0x5b, 0x9e, 0xb0, 0x9e, 0x6c, 0x4f, 0xc9, 0x05, 0x2a, 0xc5, 0xab, 0x6c, 0x83}

func admitRV64GCStructBasicWire(data []byte) bool {
	if len(data) != 373 || sha256.Sum256(data) != rv64GCStructBasicSourceDigest || !admitExactProductWireShape(data, []byte{1, 3, 6, 7, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCStructBasic(m)
}

func AdmitGCStructBasicSource(data []byte) bool { return admitRV64GCStructBasicWire(data) }

func admitRV64GCStructBasic(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 6 || len(m.FuncTypes) != 13 || len(m.Code) != 13 || len(m.Globals) != 2 || len(m.Exports) != 7 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	s := &m.Types[0].SubTypes[0]
	wantFields := []wasm.FieldType{
		wasm.NewFieldType(wasm.StorageVal(wasm.F32), wasm.Const),
		wasm.NewFieldType(wasm.StorageVal(wasm.F32), wasm.Var),
		wasm.NewFieldType(wasm.StorageVal(wasm.F32), wasm.Const),
	}
	if !s.Final || s.HasPrefix || len(s.Supers) != 0 || s.Metadata.Describes.Present() || s.Metadata.Descriptor.Present() || s.Comp.Kind != wasm.CompStruct || len(s.Comp.Fields) != len(wantFields) {
		return false
	}
	for i := range wantFields {
		if s.Comp.Fields[i] != wantFields[i] {
			return false
		}
	}
	ref0 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	wantParams := [][]wasm.ValType{nil, {ref0}, nil, {ref0, wasm.F32}, {wasm.F32}}
	wantResults := [][]wasm.ValType{{wasm.AnyRef}, {wasm.F32}, {wasm.F32}, {wasm.F32}, {wasm.F32}}
	for i := range wantParams {
		if !i31CoreFunctionType(&m.Types[i+1], wantParams[i], wantResults[i]) {
			return false
		}
	}
	wantTypes := []uint32{1, 2, 3, 2, 3, 2, 3, 2, 3, 4, 5, 4, 5}
	wantBodies := [][]byte{
		{0xfb, 1, 0, 0x0b},
		{0x20, 0, 0xfb, 2, 0, 0, 0x0b},
		{0xfb, 1, 0, 0x10, 1, 0x0b},
		{0x20, 0, 0xfb, 2, 0, 0, 0x0b},
		{0xfb, 1, 0, 0x10, 3, 0x0b},
		{0x20, 0, 0xfb, 2, 0, 1, 0x0b},
		{0xfb, 1, 0, 0x10, 5, 0x0b},
		{0x20, 0, 0xfb, 2, 0, 1, 0x0b},
		{0xfb, 1, 0, 0x10, 7, 0x0b},
		{0x20, 0, 0x20, 1, 0xfb, 5, 0, 1, 0x20, 0, 0xfb, 2, 0, 1, 0x0b},
		{0xfb, 1, 0, 0x20, 0, 0x10, 9, 0x0b},
		{0x20, 0, 0x20, 1, 0xfb, 5, 0, 1, 0x20, 0, 0xfb, 2, 0, 1, 0x0b},
		{0xfb, 1, 0, 0x20, 0, 0x10, 11, 0x0b},
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantTypes[i]}) || len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantGlobals := [][]byte{{0x43, 0, 0, 0x80, 0x3f, 0x43, 0, 0, 0, 0x40, 0x43, 0, 0, 0x40, 0x40, 0xfb, 0, 0, 0x0b}, {0xfb, 1, 0, 0x0b}}
	for i, g := range m.Globals {
		if !wasm.EqualValType(g.Type.Type, ref0) || g.Type.Mutable || len(g.Init.Instrs) != 0 || !bytes.Equal(g.Init.BodyBytes, wantGlobals[i]) {
			return false
		}
	}
	wantExports := []struct {
		name  string
		index uint32
	}{{"new", 0}, {"get_0_0", 2}, {"get_vec_0", 4}, {"get_0_y", 6}, {"get_vec_y", 8}, {"set_get_y", 10}, {"set_get_1", 12}}
	for i, want := range wantExports {
		got := m.Exports[i]
		if got.Name != want.name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != want.index {
			return false
		}
	}
	return true
}
