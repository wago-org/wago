//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCBrOnCastNullabilityHex = "0061736d01000000019680808000045f006001646e01640060016e01640060016e016300038480808000030102030abb80808000038f808080000002646e2000fb1800016e000b000b8e8080800000026e2000fb1801016e000b000b8e8080800000026e2000fb1803016e000b000b"

const rv64GCBrOnCastFailNullabilityHex = "0061736d01000000018f80808000035f006001646e01646e60016e016e038480808000030102020aba80808000038e80808000000264002000fb1900016e000b0b8e80808000000264002000fb1901016e000b0b8e80808000000263002000fb1903016e000b0b"

var (
	rv64GCBrOnCastNullabilitySourceDigest     = [sha256.Size]byte{0xcc, 0x5b, 0xde, 0xb4, 0xb5, 0x74, 0x09, 0xc6, 0xe1, 0x94, 0xae, 0x09, 0x4f, 0x2a, 0x50, 0xec, 0xe4, 0xdc, 0xe6, 0xaf, 0xa6, 0x6c, 0xd2, 0x5e, 0xc0, 0x7b, 0x46, 0x24, 0xcc, 0xb9, 0x66, 0x32}
	rv64GCBrOnCastFailNullabilitySourceDigest = [sha256.Size]byte{0x67, 0x25, 0xd0, 0xfa, 0x74, 0x19, 0x74, 0xc1, 0x21, 0x4c, 0xda, 0xbc, 0x35, 0xfa, 0x96, 0x27, 0x79, 0xf7, 0x4a, 0xbd, 0x71, 0x1c, 0xc2, 0x1b, 0x04, 0xd6, 0x7d, 0x0c, 0x40, 0x73, 0x57, 0x8e}
)

func admitRV64GCBrOnCastNullabilityWire(data []byte) bool {
	return admitRV64GCBrOnCastNullabilityWireKind(data, false)
}

func admitRV64GCBrOnCastFailNullabilityWire(data []byte) bool {
	return admitRV64GCBrOnCastNullabilityWireKind(data, true)
}

func admitRV64GCBrOnCastNullabilityWireKind(data []byte, fail bool) bool {
	wantLen, wantDigest := 111, rv64GCBrOnCastNullabilitySourceDigest
	if fail {
		wantLen, wantDigest = 103, rv64GCBrOnCastFailNullabilitySourceDigest
	}
	if len(data) != wantLen || sha256.Sum256(data) != wantDigest || !admitExactProductWireShape(data, []byte{1, 3, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCBrOnCastNullabilityKind(m, fail)
}

func AdmitGCBrOnCastNullabilitySource(data []byte) bool {
	return admitRV64GCBrOnCastNullabilityWire(data)
}

func AdmitGCBrOnCastFailNullabilitySource(data []byte) bool {
	return admitRV64GCBrOnCastFailNullabilityWire(data)
}

func admitRV64GCBrOnCastNullability(m *wasm.Module) bool {
	return admitRV64GCBrOnCastNullabilityKind(m, false)
}

func admitRV64GCBrOnCastFailNullability(m *wasm.Module) bool {
	return admitRV64GCBrOnCastNullabilityKind(m, true)
}

func admitRV64GCBrOnCastNullabilityKind(m *wasm.Module, fail bool) bool {
	wantTypes := 4
	if fail {
		wantTypes = 3
	}
	if m == nil || len(m.Types) != wantTypes || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Imports) != 0 || len(m.Tables) != 0 ||
		len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Globals) != 0 || len(m.Exports) != 0 || m.Start != nil ||
		len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	st := m.Types[0].SubTypes[0]
	if !st.Final || st.HasPrefix || len(st.Supers) != 0 || st.Metadata.Describes.Present() || st.Metadata.Descriptor.Present() || st.Comp.Kind != wasm.CompStruct || len(st.Comp.Fields) != 0 {
		return false
	}
	nonNullAny := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapAny), false))
	nonNullT := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	nullableT := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	if fail {
		if !i31CoreFunctionType(&m.Types[1], []wasm.ValType{nonNullAny}, []wasm.ValType{nonNullAny}) ||
			!i31CoreFunctionType(&m.Types[2], []wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}) {
			return false
		}
	} else if !i31CoreFunctionType(&m.Types[1], []wasm.ValType{nonNullAny}, []wasm.ValType{nonNullT}) ||
		!i31CoreFunctionType(&m.Types[2], []wasm.ValType{wasm.AnyRef}, []wasm.ValType{nonNullT}) ||
		!i31CoreFunctionType(&m.Types[3], []wasm.ValType{wasm.AnyRef}, []wasm.ValType{nullableT}) {
		return false
	}
	wantFuncTypes := []uint32{1, 2, 3}
	wantLengths := []int{14, 13, 13}
	wantDigests := [][sha256.Size]byte{
		{0x3d, 0x28, 0xbe, 0x15, 0xc3, 0x00, 0xeb, 0xc4, 0x4e, 0x74, 0xd9, 0x55, 0x1e, 0x86, 0x73, 0xe5, 0xf8, 0xbd, 0x36, 0x05, 0x4e, 0x51, 0x2e, 0xb6, 0xe0, 0xdc, 0x69, 0x5c, 0xd2, 0xb1, 0x44, 0x61},
		{0x67, 0x0f, 0x12, 0x8e, 0x1a, 0xe2, 0xa3, 0x4e, 0x5d, 0xa0, 0xc7, 0xdf, 0xe3, 0x30, 0x08, 0x37, 0x13, 0xdb, 0x63, 0x96, 0x92, 0x20, 0x1f, 0x92, 0x8c, 0x94, 0x5e, 0x77, 0xa9, 0x0e, 0x46, 0xae},
		{0xaf, 0xea, 0x2c, 0xcf, 0x85, 0x04, 0x35, 0x11, 0x42, 0x29, 0x0a, 0xab, 0x33, 0xcc, 0xbc, 0x30, 0x6f, 0x08, 0xe9, 0xc8, 0xed, 0xbc, 0xbf, 0x98, 0xbd, 0x17, 0xd2, 0x2c, 0x71, 0x34, 0x7e, 0x5f},
	}
	if fail {
		wantFuncTypes = []uint32{1, 2, 2}
		wantLengths = []int{13, 13, 13}
		wantDigests = [][sha256.Size]byte{
			{0x5f, 0x15, 0x52, 0x8e, 0x6f, 0x38, 0xce, 0x11, 0xbc, 0x85, 0x94, 0xd9, 0x1b, 0xf6, 0xbe, 0xf0, 0xf4, 0xfb, 0x43, 0x16, 0xc0, 0x42, 0xbb, 0xd7, 0xa3, 0x42, 0x9c, 0xe2, 0x98, 0x45, 0xd4, 0x75},
			{0xfc, 0xb2, 0x90, 0xa6, 0xbc, 0x01, 0x0e, 0x56, 0x57, 0x2e, 0xc4, 0xd3, 0x36, 0x42, 0xc3, 0x68, 0xc8, 0x43, 0xef, 0x98, 0xb5, 0xf4, 0x96, 0xf6, 0x6b, 0x0d, 0xfc, 0x7a, 0xc2, 0x61, 0xb6, 0x14},
			{0x31, 0x71, 0xa7, 0xec, 0x60, 0x34, 0x22, 0x19, 0xe9, 0x03, 0xb5, 0xf8, 0x7d, 0x18, 0xb4, 0x13, 0x82, 0x83, 0x19, 0x4b, 0x9b, 0xa9, 0x71, 0x36, 0xa5, 0xad, 0xd1, 0x38, 0x1f, 0x57, 0xba, 0xa6},
		}
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantFuncTypes[i]}) || len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 ||
			len(fn.BodyBytes) != wantLengths[i] || sha256.Sum256(fn.BodyBytes) != wantDigests[i] {
			return false
		}
	}
	return true
}
