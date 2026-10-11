//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCArrayInitDataHex = "0061736d01000000019880808000065e78005e78015e770160017f017f60000060037f7f7f0003878080800006030304050504069e8080800003640000410a410cfb06000b640101410cfb07010b6402004106fb07020b07f080808000060d61727261795f6765745f6e746800001161727261795f6765745f6e74685f69313600011461727261795f696e69745f646174612d6e756c6c00020f61727261795f696e69745f6461746100031361727261795f696e69745f646174615f69313600040964726f705f7365677300050c8180808000010ae0808080000689808080000023012000fb0d010b89808080000023022000fb0d020b8e8080800000d001410041004100fb1201000b8e80808000002301200020012002fb1201000b8e80808000002302200020012002fb1202000b858080800000fc09000b0b8f8080800001010c6162636465666768696a6b6c"

var rv64GCArrayInitDataSourceDigest = [sha256.Size]byte{0xc1, 0x7d, 0xa5, 0x6e, 0xd5, 0xc6, 0x50, 0x83, 0xee, 0x20, 0x02, 0x37, 0x38, 0xcc, 0x5d, 0x9a, 0x36, 0xd1, 0xe3, 0x01, 0xd2, 0xf0, 0x6f, 0x23, 0xe2, 0x64, 0x5d, 0x6e, 0xc8, 0xa9, 0xca, 0x77}

func admitRV64GCArrayInitDataWire(data []byte) bool {
	if len(data) != 335 || sha256.Sum256(data) != rv64GCArrayInitDataSourceDigest || !admitExactProductWireShape(data, []byte{1, 3, 6, 7, 12, 10, 11}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCArrayInitData(m)
}

func AdmitGCArrayInitDataSource(data []byte) bool { return admitRV64GCArrayInitDataWire(data) }

func rv64ExactPackedArrayType(group *wasm.RecType, packed wasm.PackType, mutable bool) bool {
	if group == nil || len(group.SubTypes) != 1 {
		return false
	}
	wantMut := wasm.Const
	if mutable {
		wantMut = wasm.Var
	}
	sub := &group.SubTypes[0]
	return sub.Final && !sub.HasPrefix && len(sub.Supers) == 0 && !sub.Metadata.Describes.Present() && !sub.Metadata.Descriptor.Present() &&
		sub.Comp.Kind == wasm.CompArray && sub.Comp.Array == wasm.NewFieldType(wasm.StoragePacked(packed), wantMut)
}

func admitRV64GCArrayInitData(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 6 || len(m.FuncTypes) != 6 || len(m.Code) != 6 || len(m.Globals) != 3 || len(m.Exports) != 6 || len(m.Data) != 1 || m.DataCount == nil || *m.DataCount != 1 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil ||
		len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports ||
		!rv64ExactPackedArrayType(&m.Types[0], wasm.PackI8, false) || !rv64ExactPackedArrayType(&m.Types[1], wasm.PackI8, true) || !rv64ExactPackedArrayType(&m.Types[2], wasm.PackI16, true) ||
		!i31CoreFunctionType(&m.Types[3], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}) || !i31CoreFunctionType(&m.Types[4], nil, nil) || !i31CoreFunctionType(&m.Types[5], []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil) {
		return false
	}
	wantGlobals := []struct {
		typeIndex uint32
		mutable   bool
		body      []byte
	}{{0, false, []byte{0x41, 10, 0x41, 12, 0xfb, 6, 0, 0x0b}}, {1, true, []byte{0x41, 12, 0xfb, 7, 1, 0x0b}}, {2, false, []byte{0x41, 6, 0xfb, 7, 2, 0x0b}}}
	for i, want := range wantGlobals {
		g := &m.Globals[i]
		if g.Type.Mutable != want.mutable || g.Type.Type != wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: want.typeIndex}), false)) || len(g.Init.Instrs) != 0 || !bytes.Equal(g.Init.BodyBytes, want.body) {
			return false
		}
	}
	wantFuncTypes := []uint32{3, 3, 4, 5, 5, 4}
	wantBodies := [][]byte{
		{0x23, 1, 0x20, 0, 0xfb, 0x0d, 1, 0x0b},
		{0x23, 2, 0x20, 0, 0xfb, 0x0d, 2, 0x0b},
		{0xd0, 1, 0x41, 0, 0x41, 0, 0x41, 0, 0xfb, 0x12, 1, 0, 0x0b},
		{0x23, 1, 0x20, 0, 0x20, 1, 0x20, 2, 0xfb, 0x12, 1, 0, 0x0b},
		{0x23, 2, 0x20, 0, 0x20, 1, 0x20, 2, 0xfb, 0x12, 2, 0, 0x0b},
		{0xfc, 9, 0, 0x0b},
	}
	for i := range m.Code {
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantFuncTypes[i]}) || len(m.Code[i].Locals.Runs) != 0 || m.Code[i].LocalDeclBytes != 1 || len(m.Code[i].Body.Instrs) != 0 || len(m.Code[i].Body.BodyBytes) != 0 || !bytes.Equal(m.Code[i].BodyBytes, wantBodies[i]) {
			return false
		}
	}
	if m.Data[0].Mode.Kind != wasm.DataPassive || m.Data[0].Mode.Mem != 0 || len(m.Data[0].Mode.Offset.BodyBytes) != 0 || len(m.Data[0].Mode.Offset.Instrs) != 0 || !bytes.Equal(m.Data[0].Init, []byte("abcdefghijkl")) {
		return false
	}
	wantExports := []struct {
		name  string
		index uint32
	}{{"array_get_nth", 0}, {"array_get_nth_i16", 1}, {"array_init_data-null", 2}, {"array_init_data", 3}, {"array_init_data_i16", 4}, {"drop_segs", 5}}
	for i, want := range wantExports {
		got := m.Exports[i]
		if got.Name != want.name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != want.index {
			return false
		}
	}
	return true
}
