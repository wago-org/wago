//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCBrOnCastAbstractHex = "0061736d0100000001a180808000076000017f5f0177005e780060016f0060017f017f60016b01640260016b0164010388808080000700030404040404048480808000016e000a07ca808080000604696e697400010a62725f6f6e5f6e756c6c00020962725f6f6e5f69333100030c62725f6f6e5f73747275637400040b62725f6f6e5f61727261790005096e756c6c2d64696666000609858080800001030001000ad9818080000784808080000041090bac80808000004100d06e260041014107fb1c260041024106fb00012600410341054103fb0602260041042000fb1a26000b908080800000024020002500d500417f0f0b41000b95808080000002646c20002500fb1801006e6c417f0f0bfb1e0bb2808080000002646b20002500fb1801006e6b417f0f0b02060205fb1801016b01fb1801006e02417e0f0b4100fb0d020f0bfb0301000b95808080000002646a20002500fb1801006e6a417f0f0bfb0f0b998080800000026b02646e20002500fb1803016e6b0b41000f0b41010f0b"

const rv64GCBrOnCastFailAbstractHex = "0061736d0100000001a180808000076000017f5f0177005e780060016f0060017f017f60016b01640260016b0164010388808080000700030404040404048480808000016e000a07da808080000604696e697400010e62725f6f6e5f6e6f6e5f6e756c6c00020d62725f6f6e5f6e6f6e5f69333100031062725f6f6e5f6e6f6e5f73747275637400040f62725f6f6e5f6e6f6e5f61727261790005096e756c6c2d64696666000609858080800001030001000adb818080000784808080000041090bac80808000004100d06e260041014107fb1c260041024106fb00012600410341054103fb0602260041042000fb1a26000b92808080000002646e20002500d60041000f0b417f0f0b958080800000026e20002500fb1901006e6cfb1e0f0b417f0f0bb28080800000026e20002500fb1901006e6b02060205fb1801016b01fb1801006e02417e0f0b4100fb0d020f0bfb0301000f0b417f0f0b958080800000026e20002500fb1901006e6afb0f0f0b417f0f0b99808080000002646e026b20002500fb1903016e6b0b41010f0b41000f0b"

var (
	rv64GCBrOnCastAbstractSourceDigest     = [sha256.Size]byte{0x44, 0x29, 0xdb, 0x75, 0x87, 0xba, 0x73, 0xad, 0xfc, 0x04, 0xc4, 0x4a, 0x23, 0x69, 0xba, 0xb3, 0x83, 0x43, 0xd7, 0xf5, 0x82, 0xd1, 0x74, 0x53, 0x72, 0x94, 0x0b, 0xa9, 0x6c, 0x04, 0xa2, 0x63}
	rv64GCBrOnCastFailAbstractSourceDigest = [sha256.Size]byte{0xa1, 0xd3, 0x39, 0xdf, 0xb4, 0xed, 0x4a, 0xa3, 0x08, 0xf1, 0xa9, 0xee, 0xb8, 0x29, 0x3f, 0x04, 0x75, 0xd4, 0x27, 0xb0, 0xd7, 0xae, 0xa7, 0xe8, 0xb9, 0x91, 0x59, 0xc0, 0x54, 0xfc, 0x68, 0x15}
)

func admitRV64GCBrOnCastAbstractWire(data []byte) bool {
	return admitRV64GCBrOnCastAbstractWireKind(data, false)
}

func admitRV64GCBrOnCastFailAbstractWire(data []byte) bool {
	return admitRV64GCBrOnCastAbstractWireKind(data, true)
}

func admitRV64GCBrOnCastAbstractWireKind(data []byte, fail bool) bool {
	wantLen, wantDigest := 385, rv64GCBrOnCastAbstractSourceDigest
	if fail {
		wantLen, wantDigest = 403, rv64GCBrOnCastFailAbstractSourceDigest
	}
	if len(data) != wantLen || sha256.Sum256(data) != wantDigest || !admitExactProductWireShape(data, []byte{1, 3, 4, 7, 9, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCBrOnCastAbstractKind(m, fail)
}

func AdmitGCBrOnCastAbstractSource(data []byte) bool { return admitRV64GCBrOnCastAbstractWire(data) }
func AdmitGCBrOnCastFailAbstractSource(data []byte) bool {
	return admitRV64GCBrOnCastFailAbstractWire(data)
}

func admitRV64GCBrOnCastAbstract(m *wasm.Module) bool {
	return admitRV64GCBrOnCastAbstractKind(m, false)
}

func admitRV64GCBrOnCastFailAbstract(m *wasm.Module) bool {
	return admitRV64GCBrOnCastAbstractKind(m, true)
}

func admitRV64GCBrOnCastAbstractKind(m *wasm.Module, fail bool) bool {
	if m == nil || len(m.Types) != 7 || len(m.FuncTypes) != 7 || len(m.Code) != 7 || len(m.Tables) != 1 || len(m.Exports) != 6 || len(m.Elements) != 1 ||
		len(m.Imports) != 0 || len(m.Globals) != 0 || len(m.Memories) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil ||
		len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	if !i31CoreFunctionType(&m.Types[0], nil, []wasm.ValType{wasm.I32}) || !rv64ExactPackedI8ArrayType(&m.Types[2], false) ||
		!i31CoreFunctionType(&m.Types[3], []wasm.ValType{wasm.ExternRef}, nil) || !i31CoreFunctionType(&m.Types[4], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}) {
		return false
	}
	if len(m.Types[1].SubTypes) != 1 {
		return false
	}
	st := m.Types[1].SubTypes[0]
	if !st.Final || st.HasPrefix || len(st.Supers) != 0 || st.Metadata.Describes.Present() || st.Metadata.Descriptor.Present() || st.Comp.Kind != wasm.CompStruct ||
		len(st.Comp.Fields) != 1 || st.Comp.Fields[0] != wasm.NewFieldType(wasm.StoragePacked(wasm.PackI16), wasm.Const) {
		return false
	}
	refStruct := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapStruct), false))
	refType1 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
	refType2 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 2}), false))
	if !i31CoreFunctionType(&m.Types[5], []wasm.ValType{refStruct}, []wasm.ValType{refType2}) || !i31CoreFunctionType(&m.Types[6], []wasm.ValType{refStruct}, []wasm.ValType{refType1}) {
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
	wantTypes := []uint32{0, 3, 4, 4, 4, 4, 4}
	wantLengths := []int{3, 43, 15, 20, 49, 20, 24}
	wantDigests := [][sha256.Size]byte{
		{0x83, 0x07, 0x9b, 0x75, 0xcb, 0x24, 0x43, 0x06, 0x70, 0xc9, 0x93, 0x21, 0xb5, 0xfb, 0x19, 0x73, 0x79, 0xa4, 0x0b, 0xc6, 0xeb, 0x30, 0x07, 0xdc, 0x7c, 0xd7, 0xd5, 0x0f, 0xe0, 0x53, 0x74, 0xbc},
		{0x45, 0x26, 0x43, 0x8d, 0x1e, 0x08, 0xcd, 0x46, 0xc8, 0x9e, 0x43, 0x1f, 0x27, 0x70, 0xb2, 0x7a, 0xa6, 0x90, 0x68, 0xf0, 0x78, 0x20, 0xea, 0xd7, 0xbd, 0xf3, 0x6c, 0x18, 0x15, 0x1d, 0x32, 0x93},
		{0xf8, 0x3a, 0xfc, 0x0e, 0xef, 0xa2, 0x7a, 0x59, 0x23, 0xa1, 0x68, 0x06, 0xcd, 0x6c, 0xc0, 0xbb, 0x68, 0x8d, 0x93, 0xb1, 0x98, 0x29, 0xc7, 0x6c, 0x58, 0xd8, 0x97, 0x04, 0xf4, 0x73, 0xf7, 0x1c},
		{0x20, 0x42, 0x3c, 0x9c, 0x3c, 0x5c, 0xd2, 0x56, 0x2a, 0x0f, 0xa6, 0x9c, 0xd0, 0x0d, 0x7c, 0x1b, 0x4d, 0xcc, 0x69, 0x18, 0x12, 0x3a, 0xb1, 0x99, 0x07, 0x07, 0xf0, 0x95, 0x74, 0x33, 0x70, 0x41},
		{0xed, 0xb8, 0x22, 0xf3, 0xfb, 0xa3, 0x53, 0x83, 0x87, 0x18, 0x25, 0x14, 0x3a, 0xa4, 0xef, 0xd7, 0x89, 0x88, 0xad, 0x32, 0x62, 0xd3, 0x52, 0xc0, 0x48, 0x8d, 0xee, 0x17, 0xc6, 0xba, 0x64, 0x7b},
		{0xeb, 0xa0, 0x4d, 0x3a, 0x4a, 0x31, 0xe2, 0x44, 0x74, 0xeb, 0xd0, 0xd1, 0xe1, 0xc3, 0xd0, 0x00, 0xde, 0x1d, 0x83, 0xa7, 0x60, 0x2b, 0xef, 0x62, 0xb5, 0xf9, 0xee, 0x56, 0x82, 0x37, 0x82, 0x04},
		{0xb2, 0x03, 0xd2, 0x84, 0x1b, 0x4e, 0xda, 0x01, 0xe6, 0x3f, 0x01, 0xf7, 0xce, 0x2b, 0xc7, 0xb3, 0x72, 0x34, 0xaf, 0xbf, 0x11, 0x05, 0x1b, 0xe4, 0x39, 0xe1, 0x28, 0x80, 0xe0, 0x2b, 0xa4, 0x2f},
	}
	if fail {
		wantLengths = []int{3, 43, 17, 20, 49, 20, 24}
		wantDigests = [][sha256.Size]byte{
			wantDigests[0], wantDigests[1],
			{0x83, 0x77, 0x31, 0x24, 0x40, 0x53, 0xa2, 0x71, 0xe8, 0xcc, 0x5b, 0xbc, 0x31, 0x00, 0x23, 0xb2, 0xd5, 0x40, 0x83, 0xca, 0x8e, 0x0f, 0x99, 0xcb, 0xd9, 0x67, 0xb8, 0x6d, 0x5c, 0x57, 0x90, 0x56},
			{0x50, 0x78, 0xe1, 0x53, 0x61, 0x59, 0xdb, 0xb4, 0x4c, 0x09, 0xbc, 0x34, 0xfe, 0xec, 0x14, 0x98, 0x30, 0x41, 0x31, 0x5a, 0x96, 0x68, 0xd4, 0x60, 0x86, 0xc1, 0x1d, 0x54, 0xc2, 0x7f, 0xf0, 0xf0},
			{0xd3, 0xf5, 0x90, 0x87, 0x7d, 0x32, 0x5a, 0xf8, 0x74, 0x1a, 0xf5, 0x66, 0x04, 0xa0, 0x0a, 0x49, 0x47, 0xd2, 0x55, 0x98, 0x5e, 0x1b, 0x49, 0x21, 0xaa, 0x1b, 0xbf, 0xd7, 0xa9, 0x32, 0x11, 0x47},
			{0x22, 0x01, 0xb3, 0x1a, 0x7f, 0x27, 0x68, 0xe7, 0xe7, 0xed, 0xa5, 0xc9, 0x80, 0xcb, 0xb4, 0x20, 0xd6, 0xbe, 0xea, 0x9f, 0x75, 0x13, 0x44, 0x99, 0x49, 0xf0, 0xff, 0x32, 0xc3, 0x09, 0xe6, 0xfd},
			{0x29, 0xd9, 0xda, 0xdb, 0x6b, 0x41, 0xc6, 0xfa, 0xcc, 0xc0, 0xda, 0x38, 0xfa, 0x10, 0xc5, 0x0c, 0x58, 0x7c, 0x4b, 0xdd, 0x14, 0x43, 0x18, 0x74, 0x3b, 0x54, 0xf6, 0x08, 0x59, 0xb1, 0x31, 0xf8},
		}
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantTypes[i]}) || len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || len(fn.BodyBytes) != wantLengths[i] || sha256.Sum256(fn.BodyBytes) != wantDigests[i] {
			return false
		}
	}
	wantExports := []string{"init", "br_on_null", "br_on_i31", "br_on_struct", "br_on_array", "null-diff"}
	if fail {
		wantExports = []string{"init", "br_on_non_null", "br_on_non_i31", "br_on_non_struct", "br_on_non_array", "null-diff"}
	}
	for i, name := range wantExports {
		got := m.Exports[i]
		if got.Name != name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != uint32(i+1) {
			return false
		}
	}
	return true
}
