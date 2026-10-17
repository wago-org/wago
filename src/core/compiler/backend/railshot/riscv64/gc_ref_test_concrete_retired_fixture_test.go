//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCRefTestConcreteHex = "0061736d0100000001bf808080000950005f005001005f017f005001005f017f005001015f027f007f005001025f027f007f005001005f027f007f005001005f005001065f027f007f0060000003848080800003080808048480808000016b00140799808080000208746573742d73756200010a746573742d63616e6f6e00020aca8680800003ba80808000004100fb01002600410afb010026004101fb01012600410bfb010226004102fb01032600410cfb010426004103fb010526004104fb010726000beb848080000010000240d06bfb1500450d00d000fb1500450d00d001fb1500450d00d003fb1500450d00d005fb1500450d00d007fb1500450d0041002500fb1500450d0041012500fb1500450d0041022500fb1500450d0041032500fb1500450d0041042500fb1500450d00d06bfb1501450d00d000fb1501450d00d001fb1501450d00d003fb1501450d00d005fb1501450d00d007fb1501450d0041012500fb1501450d0041022500fb1501450d00d06bfb1503450d00d000fb1503450d00d001fb1503450d00d003fb1503450d00d005fb1503450d00d007fb1503450d0041022500fb1503450d00d06bfb1505450d00d000fb1505450d00d001fb1505450d00d003fb1505450d00d005fb1505450d00d007fb1505450d0041032500fb1505450d00d06bfb1507450d00d000fb1507450d00d001fb1507450d00d003fb1507450d00d005fb1507450d00d007fb1507450d0041042500fb1507450d0041002500fb1400450d0041012500fb1400450d0041022500fb1400450d0041032500fb1400450d0041042500fb1400450d0041012500fb1401450d0041022500fb1401450d0041022500fb1403450d0041032500fb1405450d0041042500fb1407450d00d06bfb14000d00d06bfb14010d00d06bfb14030d00d06bfb14050d00d06bfb14070d0041002500fb14010d0041032500fb14010d0041042500fb14010d0041002500fb14030d0041012500fb14030d0041032500fb14030d0041042500fb14030d0041002500fb14050d0041012500fb14050d0041022500fb14050d0041042500fb14050d0041002500fb14070d0041012500fb14070d0041022500fb14070d0041032500fb14070d000f0b000b9581808000001000024041002500fb1400450d0041012500fb1400450d0041022500fb1400450d0041032500fb1400450d0041042500fb1400450d00410a2500fb1400450d00410b2500fb1400450d00410c2500fb1400450d0041012500fb1402450d0041022500fb1402450d00410b2500fb1401450d00410c2500fb1401450d0041022500fb1404450d00410c2500fb1403450d000f0b000b"

var rv64GCRefTestConcreteSourceDigest = [sha256.Size]byte{0x7a, 0x71, 0xf9, 0x66, 0x22, 0x07, 0x79, 0x9b, 0x26, 0x2c, 0xcb, 0xc7, 0x90, 0x9f, 0x4e, 0x94, 0x92, 0xc0, 0x4f, 0x71, 0x73, 0xf8, 0x4f, 0x29, 0xbe, 0x69, 0x90, 0x5d, 0x92, 0x5f, 0x64, 0x26}

func admitRV64GCRefTestConcreteShape(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 9 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Tables) != 1 || len(m.Exports) != 2 ||
		len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0]
	if table.Init != nil || table.Type.Limits.Addr64 || table.Type.Limits.HasMax || table.Type.Limits.Min != 20 || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapStruct), false))) {
		return false
	}
	wantSupers := [][]wasm.TypeIdx{nil, {{Index: 0}}, {{Index: 0}}, {{Index: 1}}, {{Index: 2}}, {{Index: 0}}, {{Index: 0}}, {{Index: 6}}}
	wantFields := []int{0, 1, 1, 2, 2, 2, 0, 2}
	for i := 0; i < 8; i++ {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
		sub := &m.Types[i].SubTypes[0]
		if sub.Final || !sub.HasPrefix || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompStruct || len(sub.Supers) != len(wantSupers[i]) || len(sub.Comp.Fields) != wantFields[i] {
			return false
		}
		for j := range wantSupers[i] {
			if sub.Supers[j] != wantSupers[i][j] {
				return false
			}
		}
		for _, field := range sub.Comp.Fields {
			if field != wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Const) {
				return false
			}
		}
	}
	if !i31CoreFunctionType(&m.Types[8], nil, nil) {
		return false
	}
	for i := range m.FuncTypes {
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: 8}) {
			return false
		}
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 {
			return false
		}
	}
	wantExports := []struct {
		name  string
		index uint32
	}{{"test-sub", 1}, {"test-canon", 2}}
	for i, want := range wantExports {
		got := m.Exports[i]
		if got.Name != want.name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != want.index {
			return false
		}
	}
	return true
}
