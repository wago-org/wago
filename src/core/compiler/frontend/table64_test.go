package frontend

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestStagedTable64ASTAdmitsGetSetGrowSizeFillAndRejectsWiderOps(t *testing.T) {
	max := uint64(4)
	base := wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Tables:    []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 2, Max: &max, Addr64: true}}}},
	}
	features := AllFeatures()
	features.Table64 = true
	for _, kind := range []wasm.InstrKind{wasm.InstrTableGet, wasm.InstrTableSet, wasm.InstrTableGrow, wasm.InstrTableSize, wasm.InstrTableFill} {
		m := base
		m.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: kind}}}}}
		if err := RejectUnsupportedWithFeatures(&m, features); err != nil {
			t.Fatalf("%s table64 AST: %v", kind, err)
		}
	}
	m := base
	m.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrCallIndirect}}}}}
	if err := RejectUnsupportedWithFeatures(&m, features); err == nil || !strings.Contains(err.Error(), "outside staged get/set/grow/size/fill family") {
		t.Fatalf("call_indirect table64 AST error = %v", err)
	}
}

func TestStagedTable64RequiresFiniteBound(t *testing.T) {
	features := AllFeatures()
	features.Table64 = true
	m := wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 1, Addr64: true}}}}}
	if err := RejectUnsupportedWithFeatures(&m, features); err == nil || !strings.Contains(err.Error(), "explicit bounded maximum") {
		t.Fatalf("unbounded table64 error = %v", err)
	}
	max := stagedTable64Max + 1
	m.Tables[0].Type.Limits.Max = &max
	if err := RejectUnsupportedWithFeatures(&m, features); err == nil || !strings.Contains(err.Error(), "exceeds staged ceiling") {
		t.Fatalf("oversized table64 error = %v", err)
	}
}
