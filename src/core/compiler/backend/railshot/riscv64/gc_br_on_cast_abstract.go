//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCBrOnCastAbstractHex = "0061736d0100000001a180808000076000017f5f0177005e780060016f0060017f017f60016b01640260016b0164010388808080000700030404040404048480808000016e000a07ca808080000604696e697400010a62725f6f6e5f6e756c6c00020962725f6f6e5f69333100030c62725f6f6e5f73747275637400040b62725f6f6e5f61727261790005096e756c6c2d64696666000609858080800001030001000ad9818080000784808080000041090bac80808000004100d06e260041014107fb1c260041024106fb00012600410341054103fb0602260041042000fb1a26000b908080800000024020002500d500417f0f0b41000b95808080000002646c20002500fb1801006e6c417f0f0bfb1e0bb2808080000002646b20002500fb1801006e6b417f0f0b02060205fb1801016b01fb1801006e02417e0f0b4100fb0d020f0bfb0301000b95808080000002646a20002500fb1801006e6a417f0f0bfb0f0b998080800000026b02646e20002500fb1803016e6b0b41000f0b41010f0b"

const rv64GCBrOnCastFailAbstractHex = "0061736d0100000001a180808000076000017f5f0177005e780060016f0060017f017f60016b01640260016b0164010388808080000700030404040404048480808000016e000a07da808080000604696e697400010e62725f6f6e5f6e6f6e5f6e756c6c00020d62725f6f6e5f6e6f6e5f69333100031062725f6f6e5f6e6f6e5f73747275637400040f62725f6f6e5f6e6f6e5f61727261790005096e756c6c2d64696666000609858080800001030001000adb818080000784808080000041090bac80808000004100d06e260041014107fb1c260041024106fb00012600410341054103fb0602260041042000fb1a26000b92808080000002646e20002500d60041000f0b417f0f0b958080800000026e20002500fb1901006e6cfb1e0f0b417f0f0bb28080800000026e20002500fb1901006e6b02060205fb1801016b01fb1801006e02417e0f0b4100fb0d020f0bfb0301000f0b417f0f0b958080800000026e20002500fb1901006e6afb0f0f0b417f0f0b99808080000002646e026b20002500fb1903016e6b0b41010f0b41000f0b"

func admitRV64GCBrOnCastAbstractWire(data []byte) bool {
	return admitRV64GCBrOnCastAbstractWireKind(data, false)
}

func admitRV64GCBrOnCastFailAbstractWire(data []byte) bool {
	return admitRV64GCBrOnCastAbstractWireKind(data, true)
}

func admitRV64GCBrOnCastAbstractWireKind(data []byte, fail bool) bool {
	wantLen := 385
	if fail {
		wantLen = 403
	}
	if len(data) != wantLen || !admitExactProductWireShape(data, []byte{1, 3, 4, 7, 9, 10}) {
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
	wantBodies := [][]byte{
		{0x41, 0x09, 0x0b},
		{0x41, 0x00, 0xd0, 0x6e, 0x26, 0x00, 0x41, 0x01, 0x41, 0x07, 0xfb, 0x1c, 0x26, 0x00, 0x41, 0x02, 0x41, 0x06, 0xfb, 0x00, 0x01, 0x26, 0x00, 0x41, 0x03, 0x41, 0x05, 0x41, 0x03, 0xfb, 0x06, 0x02, 0x26, 0x00, 0x41, 0x04, 0x20, 0x00, 0xfb, 0x1a, 0x26, 0x00, 0x0b},
		{0x02, 0x40, 0x20, 0x00, 0x25, 0x00, 0xd5, 0x00, 0x41, 0x7f, 0x0f, 0x0b, 0x41, 0x00, 0x0b},
		{0x02, 0x64, 0x6c, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x18, 0x01, 0x00, 0x6e, 0x6c, 0x41, 0x7f, 0x0f, 0x0b, 0xfb, 0x1e, 0x0b},
		{0x02, 0x64, 0x6b, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x18, 0x01, 0x00, 0x6e, 0x6b, 0x41, 0x7f, 0x0f, 0x0b, 0x02, 0x06, 0x02, 0x05, 0xfb, 0x18, 0x01, 0x01, 0x6b, 0x01, 0xfb, 0x18, 0x01, 0x00, 0x6e, 0x02, 0x41, 0x7e, 0x0f, 0x0b, 0x41, 0x00, 0xfb, 0x0d, 0x02, 0x0f, 0x0b, 0xfb, 0x03, 0x01, 0x00, 0x0b},
		{0x02, 0x64, 0x6a, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x18, 0x01, 0x00, 0x6e, 0x6a, 0x41, 0x7f, 0x0f, 0x0b, 0xfb, 0x0f, 0x0b},
		{0x02, 0x6b, 0x02, 0x64, 0x6e, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x18, 0x03, 0x01, 0x6e, 0x6b, 0x0b, 0x41, 0x00, 0x0f, 0x0b, 0x41, 0x01, 0x0f, 0x0b},
	}
	if fail {
		wantBodies = [][]byte{
			wantBodies[0], wantBodies[1],
			{0x02, 0x64, 0x6e, 0x20, 0x00, 0x25, 0x00, 0xd6, 0x00, 0x41, 0x00, 0x0f, 0x0b, 0x41, 0x7f, 0x0f, 0x0b},
			{0x02, 0x6e, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x19, 0x01, 0x00, 0x6e, 0x6c, 0xfb, 0x1e, 0x0f, 0x0b, 0x41, 0x7f, 0x0f, 0x0b},
			{0x02, 0x6e, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x19, 0x01, 0x00, 0x6e, 0x6b, 0x02, 0x06, 0x02, 0x05, 0xfb, 0x18, 0x01, 0x01, 0x6b, 0x01, 0xfb, 0x18, 0x01, 0x00, 0x6e, 0x02, 0x41, 0x7e, 0x0f, 0x0b, 0x41, 0x00, 0xfb, 0x0d, 0x02, 0x0f, 0x0b, 0xfb, 0x03, 0x01, 0x00, 0x0f, 0x0b, 0x41, 0x7f, 0x0f, 0x0b},
			{0x02, 0x6e, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x19, 0x01, 0x00, 0x6e, 0x6a, 0xfb, 0x0f, 0x0f, 0x0b, 0x41, 0x7f, 0x0f, 0x0b},
			{0x02, 0x64, 0x6e, 0x02, 0x6b, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x19, 0x03, 0x01, 0x6e, 0x6b, 0x0b, 0x41, 0x01, 0x0f, 0x0b, 0x41, 0x00, 0x0f, 0x0b},
		}
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantTypes[i]}) || len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
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
