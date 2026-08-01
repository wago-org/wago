package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGenericStructHelpersIgnoreAbstractOpsWithoutStructTypes(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		Code:  []wasm.Func{{BodyBytes: []byte{0xfb, 0x14, 0x6c, 0x0b}}}, // ref.test i31; end
	}
	if moduleUsesGenericGCStructHelpers(m) {
		t.Fatal("abstract i31 ref.test without struct types selected generic struct helpers")
	}
	m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompStruct}}}})
	if !moduleUsesGenericGCStructHelpers(m) {
		t.Fatal("abstract ref.test with a struct type did not select generic struct helpers")
	}
}

func TestModuleSegmentStateCountsIncludeGCArraySegments(t *testing.T) {
	m := &wasm.Module{
		Elements: make([]wasm.Elem, 4),
		Data:     make([]wasm.Data, 5),
		Code: []wasm.Func{{BodyBytes: []byte{
			0xfb, 0x0a, 0x00, 0x03, // array.new_elem type 0, element segment 3
			0xfb, 0x09, 0x00, 0x04, // array.new_data type 0, data segment 4
			0x0b,
		}}},
	}
	elemCount, dataCount := moduleSegmentStateCounts(m)
	if elemCount != 4 || dataCount != 5 {
		t.Fatalf("segment state counts = %d/%d, want 4/5", elemCount, dataCount)
	}
	requirements := analyzeModuleRequirements(m)
	if requirements.elemStateCount != elemCount || requirements.dataStateCount != dataCount {
		t.Fatalf("fused segment state counts = %d/%d, want %d/%d", requirements.elemStateCount, requirements.dataStateCount, elemCount, dataCount)
	}

	programmatic := &wasm.Module{
		Elements: make([]wasm.Elem, 3),
		Code: []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{
			Kind:  wasm.InstrElemDrop,
			Index: 2,
		}}}}},
	}
	requirements = analyzeModuleRequirements(programmatic)
	if requirements.elemStateCount != 3 {
		t.Fatalf("programmatic fused element state count = %d, want 3", requirements.elemStateCount)
	}
}
