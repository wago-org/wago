package frontend

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestStagedTable64ASTAdmitsGetSetGrowSizeFillCallIndirectAndRejectsWiderOps(t *testing.T) {
	max := uint64(4)
	base := wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Tables:    []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 2, Max: &max, Addr64: true}}}},
	}
	features := AllFeatures()
	features.Table64 = true
	for _, kind := range []wasm.InstrKind{wasm.InstrTableGet, wasm.InstrTableSet, wasm.InstrTableGrow, wasm.InstrTableSize, wasm.InstrTableFill, wasm.InstrCallIndirect} {
		m := base
		m.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: kind}}}}}
		if err := RejectUnsupportedWithFeatures(&m, features); err != nil {
			t.Fatalf("%s table64 AST: %v", kind, err)
		}
	}
	m := base
	m.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrTableCopy}}}}}
	if err := RejectUnsupportedWithFeatures(&m, features); err == nil || !strings.Contains(err.Error(), "outside staged get/set/grow/size/fill family") {
		t.Fatalf("table.copy table64 AST error = %v", err)
	}
}

func TestStagedTable64ASTAdmitsInitializerAndI64ActiveElement(t *testing.T) {
	max := uint64(4)
	m := wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Tables: []wasm.Table{{
			Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 2, Max: &max, Addr64: true}},
			Init: &wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefFunc, Index: 0}}},
		}},
		Elements: []wasm.Elem{{
			Mode: wasm.ElemMode{Kind: wasm.ElemActive, Table: 0, Offset: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI64Const, I64: 1}}}},
			Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}},
		}},
	}
	features := AllFeatures()
	features.Table64 = true
	if err := RejectUnsupportedWithFeatures(&m, features); err != nil {
		t.Fatalf("table64 initializer/active element AST: %v", err)
	}
}

func TestStagedTable64RequiresFiniteRuntimeBound(t *testing.T) {
	features := AllFeatures()
	features.Table64 = true
	m := wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 1, Addr64: true}}}}}
	if err := RejectUnsupportedWithFeatures(&m, features); err != nil {
		t.Fatalf("private non-growing no-max table64: %v", err)
	}
	m.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrTableGrow}}}}}
	if err := RejectUnsupportedWithFeatures(&m, features); err == nil || !strings.Contains(err.Error(), "private and non-growing") {
		t.Fatalf("growing no-max table64 error = %v", err)
	}
	m.Code = nil
	m.Exports = []wasm.Export{{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}}}
	if err := RejectUnsupportedWithFeatures(&m, features); err == nil || !strings.Contains(err.Error(), "private and non-growing") {
		t.Fatalf("exported no-max table64 error = %v", err)
	}
	m.Exports = nil
	max := stagedTable64Max + 1
	m.Tables[0].Type.Limits.Max = &max
	if err := RejectUnsupportedWithFeatures(&m, features); err == nil || !strings.Contains(err.Error(), "exceeds staged ceiling") {
		t.Fatalf("oversized table64 error = %v", err)
	}
}
