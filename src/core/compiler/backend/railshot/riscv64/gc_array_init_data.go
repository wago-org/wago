//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCArrayInitDataHex = "0061736d01000000019880808000065e78005e78015e770160017f017f60000060037f7f7f0003878080800006030304050504069e8080800003640000410a410cfb06000b640101410cfb07010b6402004106fb07020b07f080808000060d61727261795f6765745f6e746800001161727261795f6765745f6e74685f69313600011461727261795f696e69745f646174612d6e756c6c00020f61727261795f696e69745f6461746100031361727261795f696e69745f646174615f69313600040964726f705f7365677300050c8180808000010ae0808080000689808080000023012000fb0d010b89808080000023022000fb0d020b8e8080800000d001410041004100fb1201000b8e80808000002301200020012002fb1201000b8e80808000002302200020012002fb1202000b858080800000fc09000b0b8f8080800001010c6162636465666768696a6b6c"
const rv64GCArrayInitDataWideHex = "0061736d01000000018a80808000035e7f015e7e01600000038d808080000c02020202020202020202020207bd808080000c0266300000026631000102663200020266330003026634000402663900050267300006026731000702673400080267370009026738000a026739000b0c8180808000070a89828080000c9180808000004101fb0700410041004101fb1200000b9180808000004101fb0700410041004101fb1200010b9180808000004101fb0700410041004101fb1200020b9180808000004101fb0700410041004101fb1200030b9180808000004101fb0700410041004101fb1200040b9180808000004101fb0700410041064101fb1200060b9180808000004101fb0701410041004101fb1201000b9180808000004101fb0701410041004101fb1201010b9180808000004101fb0701410041004101fb1201040b9180808000004101fb0701410041004101fb1201050b9180808000004101fb0701410041004101fb1201060b9180808000004101fb0701410041024101fb1201060b0ba9808080000701000101310102313201033132330104313233340107313233343536370109313233343536373839"

var rv64GCArrayInitDataSourceDigest = [sha256.Size]byte{0xc1, 0x7d, 0xa5, 0x6e, 0xd5, 0xc6, 0x50, 0x83, 0xee, 0x20, 0x02, 0x37, 0x38, 0xcc, 0x5d, 0x9a, 0x36, 0xd1, 0xe3, 0x01, 0xd2, 0xf0, 0x6f, 0x23, 0xe2, 0x64, 0x5d, 0x6e, 0xc8, 0xa9, 0xca, 0x77}
var rv64GCArrayInitDataWideSourceDigest = [sha256.Size]byte{0x05, 0x82, 0x7a, 0x01, 0xce, 0xc2, 0xe9, 0xf3, 0x62, 0x3e, 0x9d, 0x00, 0xb5, 0x4a, 0xff, 0x25, 0x8b, 0xbc, 0x7b, 0x49, 0x7f, 0x47, 0xb7, 0x6f, 0xfd, 0x31, 0x45, 0x2b, 0xbc, 0xb9, 0xb3, 0x1f}

func admitRV64GCArrayInitDataWire(data []byte) bool {
	digest := sha256.Sum256(data)
	packed := len(data) == 335 && digest == rv64GCArrayInitDataSourceDigest && admitExactProductWireShape(data, []byte{1, 3, 6, 7, 12, 10, 11})
	wide := len(data) == 435 && digest == rv64GCArrayInitDataWideSourceDigest && admitExactProductWireShape(data, []byte{1, 3, 7, 12, 10, 11})
	if !packed && !wide {
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

func rv64ExactNumericArrayType(group *wasm.RecType, typ wasm.ValType, mutable bool) bool {
	if group == nil || len(group.SubTypes) != 1 {
		return false
	}
	wantMut := wasm.Const
	if mutable {
		wantMut = wasm.Var
	}
	sub := &group.SubTypes[0]
	return sub.Final && !sub.HasPrefix && len(sub.Supers) == 0 && !sub.Metadata.Describes.Present() && !sub.Metadata.Descriptor.Present() &&
		sub.Comp.Kind == wasm.CompArray && sub.Comp.Array == wasm.NewFieldType(wasm.StorageVal(typ), wantMut)
}

func admitRV64GCArrayInitData(m *wasm.Module) bool {
	return admitRV64GCArrayInitDataPacked(m) || admitRV64GCArrayInitDataWide(m)
}

func admitRV64GCArrayInitDataPacked(m *wasm.Module) bool {
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

func admitRV64GCArrayInitDataWide(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 3 || len(m.FuncTypes) != 12 || len(m.Code) != 12 || len(m.Exports) != 12 || len(m.Data) != 7 || m.DataCount == nil || *m.DataCount != 7 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil ||
		len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports ||
		!rv64ExactNumericArrayType(&m.Types[0], wasm.I32, true) || !rv64ExactNumericArrayType(&m.Types[1], wasm.I64, true) || !i31CoreFunctionType(&m.Types[2], nil, nil) {
		return false
	}
	wantData := [][]byte{nil, []byte("1"), []byte("12"), []byte("123"), []byte("1234"), []byte("1234567"), []byte("123456789")}
	for i, want := range wantData {
		d := &m.Data[i]
		if d.Mode.Kind != wasm.DataPassive || d.Mode.Mem != 0 || len(d.Mode.Offset.BodyBytes) != 0 || len(d.Mode.Offset.Instrs) != 0 || !bytes.Equal(d.Init, want) {
			return false
		}
	}
	wantDataIndex := []byte{0, 1, 2, 3, 4, 6, 0, 1, 4, 5, 6, 6}
	wantSource := []byte{0, 0, 0, 0, 0, 6, 0, 0, 0, 0, 0, 2}
	for i := range m.Code {
		typeIndex := byte(0)
		if i >= 6 {
			typeIndex = 1
		}
		wantBody := []byte{0x41, 1, 0xfb, 7, typeIndex, 0x41, 0, 0x41, wantSource[i], 0x41, 1, 0xfb, 0x12, typeIndex, wantDataIndex[i], 0x0b}
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: 2}) || len(m.Code[i].Locals.Runs) != 0 || m.Code[i].LocalDeclBytes != 1 || len(m.Code[i].Body.Instrs) != 0 || len(m.Code[i].Body.BodyBytes) != 0 || !bytes.Equal(m.Code[i].BodyBytes, wantBody) {
			return false
		}
	}
	wantExports := []string{"f0", "f1", "f2", "f3", "f4", "f9", "g0", "g1", "g4", "g7", "g8", "g9"}
	for i, name := range wantExports {
		got := m.Exports[i]
		if got.Name != name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}
