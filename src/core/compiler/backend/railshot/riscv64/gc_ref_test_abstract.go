//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCRefTestAbstractHex = "0061736d01000000019280808000056000005f005e780060016f0060017f017f038d808080000c000304040404040404040404048a80808000036e000a70000a6f000a07ba818080000b04696e69740001127265665f746573745f6e756c6c5f6461746100020c7265665f746573745f616e7900030b7265665f746573745f657100040c7265665f746573745f69333100050f7265665f746573745f73747275637400060e7265665f746573745f61727261790007127265665f746573745f6e756c6c5f66756e6300080d7265665f746573745f66756e630009147265665f746573745f6e756c6c5f65787465726e000a0f7265665f746573745f65787465726e000b09858080800001030001000ade828080000c8280808000000bfb80808000004100d06e26004101d06b26004102d071260041034107fb1c26004104fb0101260041054100fb0702260041062000fb1a26004107d06ffb1a26004100d07326014101d07026014102d20026014100d07226024101d06f260241022000260241034108fb1cfb1b26024104fb0101fb1b26024105d06efb1b26020b8f808080000020002500d120002500fb15716a0b91808080000020002500fb146e20002500fb156e6a0b91808080000020002500fb146d20002500fb156d6a0b91808080000020002500fb146c20002500fb156c6a0b91808080000020002500fb146b20002500fb156b6a0b91808080000020002500fb146a20002500fb156a6a0b8f808080000020002501d120002501fb15736a0b91808080000020002501fb147020002501fb15706a0b8f808080000020002502d120002502fb15726a0b91808080000020002502fb146f20002502fb156f6a0b"

func admitRV64GCRefTestAbstractWire(data []byte) bool {
	if len(data) != 626 {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCRefTestAbstract(m)
}

// AdmitGCRefTestAbstractSource is the exact-wire witness for the official
// mixed abstract anyref/funcref/externref ref.test checkpoint.
func AdmitGCRefTestAbstractSource(data []byte) bool { return admitRV64GCRefTestAbstractWire(data) }

func admitRV64GCRefTestAbstract(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 5 || len(m.FuncTypes) != 12 || len(m.Code) != 12 || len(m.Tables) != 3 || len(m.Exports) != 11 || len(m.Elements) != 1 ||
		len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	structType := &m.Types[1]
	if len(structType.SubTypes) != 1 {
		return false
	}
	structSub := &structType.SubTypes[0]
	if !i31CoreFunctionType(&m.Types[0], nil, nil) || !structSub.Final || structSub.HasPrefix || len(structSub.Supers) != 0 || structSub.Metadata.Describes.Present() || structSub.Metadata.Descriptor.Present() || structSub.Comp.Kind != wasm.CompStruct || len(structSub.Comp.Fields) != 0 || !rv64ExactPackedI8ArrayType(&m.Types[2], false) ||
		!i31CoreFunctionType(&m.Types[3], []wasm.ValType{wasm.ExternRef}, nil) || !i31CoreFunctionType(&m.Types[4], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}) {
		return false
	}
	wantTables := []wasm.ValType{wasm.AnyRef, wasm.FuncRef, wasm.ExternRef}
	for i, table := range m.Tables {
		if table.Init != nil || table.Type.Limits.Addr64 || table.Type.Limits.HasMax || table.Type.Limits.Min != 10 || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wantTables[i]) {
			return false
		}
	}
	if m.FuncTypes[0] != (wasm.TypeIdx{Index: 0}) || m.FuncTypes[1] != (wasm.TypeIdx{Index: 3}) {
		return false
	}
	for i := 2; i < len(m.FuncTypes); i++ {
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: 4}) {
			return false
		}
	}
	elem := &m.Elements[0]
	if elem.Mode.Kind != wasm.ElemDeclarative || elem.Mode.Table != 0 || len(elem.Mode.Offset.Instrs) != 0 || len(elem.Mode.Offset.BodyBytes) != 0 || elem.Kind.Kind != wasm.ElemFuncs || len(elem.Kind.Funcs) != 1 || elem.Kind.Funcs[0] != 0 || len(elem.Kind.Exprs) != 0 {
		return false
	}
	wantBodies := [][]byte{
		{0x0b},
		{0x41, 0x00, 0xd0, 0x6e, 0x26, 0x00, 0x41, 0x01, 0xd0, 0x6b, 0x26, 0x00, 0x41, 0x02, 0xd0, 0x71, 0x26, 0x00, 0x41, 0x03, 0x41, 0x07, 0xfb, 0x1c, 0x26, 0x00, 0x41, 0x04, 0xfb, 0x01, 0x01, 0x26, 0x00, 0x41, 0x05, 0x41, 0x00, 0xfb, 0x07, 0x02, 0x26, 0x00, 0x41, 0x06, 0x20, 0x00, 0xfb, 0x1a, 0x26, 0x00, 0x41, 0x07, 0xd0, 0x6f, 0xfb, 0x1a, 0x26, 0x00, 0x41, 0x00, 0xd0, 0x73, 0x26, 0x01, 0x41, 0x01, 0xd0, 0x70, 0x26, 0x01, 0x41, 0x02, 0xd2, 0x00, 0x26, 0x01, 0x41, 0x00, 0xd0, 0x72, 0x26, 0x02, 0x41, 0x01, 0xd0, 0x6f, 0x26, 0x02, 0x41, 0x02, 0x20, 0x00, 0x26, 0x02, 0x41, 0x03, 0x41, 0x08, 0xfb, 0x1c, 0xfb, 0x1b, 0x26, 0x02, 0x41, 0x04, 0xfb, 0x01, 0x01, 0xfb, 0x1b, 0x26, 0x02, 0x41, 0x05, 0xd0, 0x6e, 0xfb, 0x1b, 0x26, 0x02, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xd1, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x15, 0x71, 0x6a, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x14, 0x6e, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x15, 0x6e, 0x6a, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x14, 0x6d, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x15, 0x6d, 0x6a, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x14, 0x6c, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x15, 0x6c, 0x6a, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x14, 0x6b, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x15, 0x6b, 0x6a, 0x0b},
		{0x20, 0x00, 0x25, 0x00, 0xfb, 0x14, 0x6a, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x15, 0x6a, 0x6a, 0x0b},
		{0x20, 0x00, 0x25, 0x01, 0xd1, 0x20, 0x00, 0x25, 0x01, 0xfb, 0x15, 0x73, 0x6a, 0x0b},
		{0x20, 0x00, 0x25, 0x01, 0xfb, 0x14, 0x70, 0x20, 0x00, 0x25, 0x01, 0xfb, 0x15, 0x70, 0x6a, 0x0b},
		{0x20, 0x00, 0x25, 0x02, 0xd1, 0x20, 0x00, 0x25, 0x02, 0xfb, 0x15, 0x72, 0x6a, 0x0b},
		{0x20, 0x00, 0x25, 0x02, 0xfb, 0x14, 0x6f, 0x20, 0x00, 0x25, 0x02, 0xfb, 0x15, 0x6f, 0x6a, 0x0b},
	}
	for i, fn := range m.Code {
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []string{"init", "ref_test_null_data", "ref_test_any", "ref_test_eq", "ref_test_i31", "ref_test_struct", "ref_test_array", "ref_test_null_func", "ref_test_func", "ref_test_null_extern", "ref_test_extern"}
	for i, name := range wantExports {
		got := m.Exports[i]
		if got.Name != name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != uint32(i+1) {
			return false
		}
	}
	return true
}
