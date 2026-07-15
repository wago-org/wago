package frontend

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestStagedMemory64ASTAdmitsIntegerScalarsAndRejectsFloat(t *testing.T) {
	max := uint64(2)
	base := wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Memories:  []wasm.MemType{{Limits: wasm.Limits{Min: 1, Max: &max, Addr64: true}}},
	}
	feat := AllFeatures()
	feat.Memory64 = true

	integer := base
	integer.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI64Load}, {Kind: wasm.InstrDrop}}}}}
	if err := RejectUnsupportedWithFeatures(&integer, feat); err != nil {
		t.Fatalf("integer memory64 AST: %v", err)
	}

	floating := base
	floating.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrF32Load}, {Kind: wasm.InstrDrop}}}}}
	if err := RejectUnsupportedWithFeatures(&floating, feat); err == nil || !strings.Contains(err.Error(), "outside staged integer scalar family") {
		t.Fatalf("floating memory64 AST error = %v", err)
	}
}
