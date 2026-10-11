//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCRefTestTableHex = "0061736d01000000019b808080000550005f005001005f017f005001005f017e0060000060017f017f0385808080000403040404048480808000016b0002079e808080000404696e6974000004626173650001046c656674000205726967687400030ac080808000049080808000004100fb010126004101fb010226000b89808080000020002500fb14000b89808080000020002500fb14010b89808080000020002500fb14020b"

var rv64GCRefTestTableSourceDigest = [sha256.Size]byte{0xab, 0x93, 0xf4, 0x6c, 0x27, 0x1d, 0x3e, 0x1a, 0x71, 0xc2, 0x1d, 0xa7, 0x25, 0x7e, 0x29, 0xb2, 0x36, 0x3e, 0x91, 0x88, 0x72, 0x53, 0x78, 0x00, 0x57, 0x05, 0xb3, 0x3a, 0x05, 0x6a, 0x8c, 0xbd}

func admitRV64GCRefTestTableWire(data []byte) bool {
	if len(data) != 168 || sha256.Sum256(data) != rv64GCRefTestTableSourceDigest {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCRefTestTable(m)
}

// AdmitGCRefTestTableSource is the exact-wire witness for the bounded rooted
// object-table allocation and ref.test checkpoint.
func AdmitGCRefTestTableSource(data []byte) bool { return admitRV64GCRefTestTableWire(data) }

func admitRV64GCRefTestTable(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 5 || len(m.FuncTypes) != 4 || len(m.Code) != 4 || len(m.Tables) != 1 || len(m.Exports) != 4 ||
		len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0]
	if table.Init != nil || table.Type.Limits.Addr64 || table.Type.Limits.HasMax || table.Type.Limits.Min != 2 || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapStruct), false))) {
		return false
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := &m.Types[i].SubTypes[0]
		if sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() {
			return false
		}
		switch i {
		case 0:
			if sub.Final || !sub.HasPrefix || len(sub.Supers) != 0 || sub.Comp.Kind != wasm.CompStruct || len(sub.Comp.Fields) != 0 {
				return false
			}
		case 1, 2:
			field := wasm.I32
			if i == 2 {
				field = wasm.I64
			}
			if sub.Final || !sub.HasPrefix || len(sub.Supers) != 1 || sub.Supers[0] != (wasm.TypeIdx{Index: 0}) || sub.Comp.Kind != wasm.CompStruct || len(sub.Comp.Fields) != 1 || sub.Comp.Fields[0] != wasm.NewFieldType(wasm.StorageVal(field), wasm.Const) {
				return false
			}
		case 3:
			if !i31CoreFunctionType(&m.Types[i], nil, nil) {
				return false
			}
		case 4:
			if !i31CoreFunctionType(&m.Types[i], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}) {
				return false
			}
		}
	}
	if m.FuncTypes[0] != (wasm.TypeIdx{Index: 3}) || m.FuncTypes[1] != (wasm.TypeIdx{Index: 4}) || m.FuncTypes[2] != (wasm.TypeIdx{Index: 4}) || m.FuncTypes[3] != (wasm.TypeIdx{Index: 4}) {
		return false
	}
	wantBodies := [][]byte{
		{0x41, 0, 0xfb, 1, 1, 0x26, 0, 0x41, 1, 0xfb, 1, 2, 0x26, 0, 0x0b},
		{0x20, 0, 0x25, 0, 0xfb, 0x14, 0, 0x0b},
		{0x20, 0, 0x25, 0, 0xfb, 0x14, 1, 0x0b},
		{0x20, 0, 0x25, 0, 0xfb, 0x14, 2, 0x0b},
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []struct {
		name  string
		index uint32
	}{{"init", 0}, {"base", 1}, {"left", 2}, {"right", 3}}
	for i, want := range wantExports {
		got := m.Exports[i]
		if got.Name != want.name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != want.index {
			return false
		}
	}
	return true
}
