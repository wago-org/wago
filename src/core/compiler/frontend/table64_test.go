package frontend

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestStagedTable64ASTAdmitsGetSetGrowSizeFillCopyAndCallIndirect(t *testing.T) {
	max := uint64(4)
	base := wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Tables:    []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 2, Max: &max, Addr64: true}}}},
	}
	features := AllFeatures()
	features.Table64 = true
	for _, kind := range []wasm.InstrKind{wasm.InstrTableGet, wasm.InstrTableSet, wasm.InstrTableGrow, wasm.InstrTableSize, wasm.InstrTableFill, wasm.InstrTableCopy, wasm.InstrCallIndirect} {
		m := base
		m.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: kind}}}}}
		if err := RejectUnsupportedWithFeatures(&m, features); err != nil {
			t.Fatalf("%s table64 AST: %v", kind, err)
		}
	}
	imported := base
	imported.Tables = nil
	imported.Imports = []wasm.Import{{Module: "env", Name: "table", Type: wasm.ExternType{Kind: wasm.ExternTable, Table: base.Tables[0].Type}}}
	imported.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrTableCopy}}}}}
	if err := RejectUnsupportedWithFeatures(&imported, features); err == nil || !strings.Contains(err.Error(), "imported table64") {
		t.Fatalf("imported table64.copy gate = %v", err)
	}
}

func TestStagedTable64ASTAdmitsPassiveInitAndDrop(t *testing.T) {
	max := uint64(4)
	m := wasm.Module{
		Tables: []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 2, Max: &max, Addr64: true}}}},
		Elements: []wasm.Elem{
			{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs}},
			{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs}},
		},
		Code: []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{
			{Kind: wasm.InstrTableInit, Index: 0, Index2: 0},
			{Kind: wasm.InstrElemDrop, Index: 0},
		}}}},
	}
	features := AllFeatures()
	features.Table64 = true
	if err := RejectUnsupportedWithFeatures(&m, features); err != nil {
		t.Fatalf("table64 passive init/drop AST: %v", err)
	}
	m.Imports = []wasm.Import{{Module: "env", Name: "table", Type: wasm.ExternType{Kind: wasm.ExternTable, Table: m.Tables[0].Type}}}
	m.Tables = nil
	if err := RejectUnsupportedWithFeatures(&m, features); err == nil || !strings.Contains(err.Error(), "imported table64") {
		t.Fatalf("imported table64.init gate = %v", err)
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
	if err := RejectUnsupportedWithFeatures(&m, features); err != nil {
		t.Fatalf("growing no-max table64 with bounded reservation: %v", err)
	}
	m.Code = nil
	m.Exports = []wasm.Export{{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}}}
	if err := RejectUnsupportedWithFeatures(&m, features); err != nil {
		t.Fatalf("exported no-max table64 with bounded reservation: %v", err)
	}
	m.Exports = nil
	max := stagedTable64Max + 1
	m.Tables[0].Type.Limits.Max = &max
	if err := RejectUnsupportedWithFeatures(&m, features); err == nil || !strings.Contains(err.Error(), "exceeds staged ceiling") {
		t.Fatalf("oversized table64 error = %v", err)
	}
}

func TestStagedTable64ImportBounds(t *testing.T) {
	features := AllFeatures()
	features.Table64 = true
	max := uint64(4)
	m := wasm.Module{Imports: []wasm.Import{{
		Module: "env", Name: "table", Type: wasm.ExternType{Kind: wasm.ExternTable, Table: wasm.TableType{
			Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 2, Max: &max, Addr64: true},
		}},
	}}}
	if err := RejectUnsupportedWithFeatures(&m, features); err != nil {
		t.Fatalf("bounded table64 import: %v", err)
	}
	m.Imports[0].Type.Table.Limits.Max = nil
	if err := RejectUnsupportedWithFeatures(&m, features); err != nil {
		t.Fatalf("no-max table64 import: %v", err)
	}
	tooLarge := stagedTable64Max + 1
	m.Imports[0].Type.Table.Limits.Max = &tooLarge
	if err := RejectUnsupportedWithFeatures(&m, features); err == nil || !strings.Contains(err.Error(), "exceeds staged ceiling") {
		t.Fatalf("oversized table64 import error = %v", err)
	}
}
