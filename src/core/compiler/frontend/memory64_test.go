package frontend

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestStagedMemory64ASTAdmitsScalarSIMDAndBoundedBulk(t *testing.T) {
	max := uint64(2)
	base := wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Memories:  []wasm.MemType{{Limits: wasm.Limits{Min: 1, Max: &max, Addr64: true}}},
	}
	feat := AllFeatures()
	feat.Memory64 = true
	feat.SIMD = true

	integer := base
	integer.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI64Load}, {Kind: wasm.InstrDrop}}}}}
	if err := RejectUnsupportedWithFeatures(&integer, feat); err != nil {
		t.Fatalf("integer memory64 AST: %v", err)
	}

	floating := base
	floating.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrF32Load}, {Kind: wasm.InstrDrop}}}}}
	if err := RejectUnsupportedWithFeatures(&floating, feat); err != nil {
		t.Fatalf("floating memory64 AST: %v", err)
	}

	simd := base
	simd.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrV128Load}, {Kind: wasm.InstrDrop}}}}}
	if err := RejectUnsupportedWithFeatures(&simd, feat); err != nil {
		t.Fatalf("SIMD memory64 AST: %v", err)
	}

	for _, kind := range []wasm.InstrKind{wasm.InstrMemoryCopy, wasm.InstrMemoryFill} {
		bulk := base
		bulk.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: kind}}}}}
		if err := RejectUnsupportedWithFeatures(&bulk, feat); err != nil {
			t.Fatalf("%s memory64 AST: %v", kind, err)
		}
	}

	init := base
	init.DataCount = new(uint32)
	init.Data = []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataPassive}}}
	init.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrMemoryInit}, {Kind: wasm.InstrDataDrop}}}}}
	if err := RejectUnsupportedWithFeatures(&init, feat); err != nil {
		t.Fatalf("memory64 passive lifecycle AST: %v", err)
	}
}
