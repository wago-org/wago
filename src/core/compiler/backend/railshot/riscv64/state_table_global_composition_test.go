//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func stateOnlyTable32ScalarGlobalsProfile() *wasm.Module {
	return &wasm.Module{
		Tables: []wasm.Table{{Type: wasm.TableType{
			Ref:    wasm.AbsRef(wasm.HeapFunc),
			Limits: wasm.Limits{Min: 2, Max: 5, HasMax: true},
		}}},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{Type: wasm.I32}, Init: wasm.Expr{BodyBytes: []byte{0x41, 0x7f, 0x0b}}},
			{Type: wasm.GlobalType{Type: wasm.I64}, Init: wasm.Expr{BodyBytes: []byte{0x42, 0x7e, 0x0b}}},
			{Type: wasm.GlobalType{Type: wasm.F32}, Init: wasm.Expr{BodyBytes: []byte{0x43, 0, 0, 0, 0x80, 0x0b}}},
			{Type: wasm.GlobalType{Type: wasm.F64}, Init: wasm.Expr{BodyBytes: []byte{0x44, 0x42, 0, 0, 0, 0, 0, 0xf8, 0x7f, 0x0b}}},
		},
		Exports: []wasm.Export{
			{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
			{Name: "i32", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 0}},
			{Name: "i64", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 1}},
			{Name: "f32", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 2}},
			{Name: "f64", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 3}},
		},
	}
}

func TestStateOnlyTable32ScalarGlobalsProfile(t *testing.T) {
	m := stateOnlyTable32ScalarGlobalsProfile()
	if !admitStateOnlyTable32ScalarGlobals(m, true, true) {
		t.Fatal("exact table32/scalar-global composition rejected")
	}
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("table32/scalar-global table admission", table, err)
	}
	state, err := admitStatePolicy(m, true, false, true, true)
	if err != nil || !state.ownedTable || len(state.globals) != 4 {
		t.Fatal("table32/scalar-global state admission", state.ownedTable, len(state.globals), err)
	}
	want := []uint64{0xffffffff, 0xfffffffffffffffe, 0x80000000, 0x7ff8000000000042}
	for i, global := range state.globals {
		if global.mutable || global.value != want[i] {
			t.Fatal("scalar literal bits", i, global, want[i])
		}
	}
	for _, tc := range []struct {
		production bool
		public     bool
	}{{false, true}, {true, false}, {false, false}} {
		if admitStateOnlyTable32ScalarGlobals(m, tc.production, tc.public) {
			t.Fatal("composition escaped production/public boundary", tc)
		}
	}
}

func TestStateOnlyTable32ScalarGlobalsProfileRejectsOtherDeclarations(t *testing.T) {
	for name, mutate := range map[string]func(*wasm.Module){
		"import":       func(m *wasm.Module) { m.Imports = []wasm.Import{{}} },
		"type":         func(m *wasm.Module) { m.Types = []wasm.RecType{{}} },
		"function":     func(m *wasm.Module) { m.FuncTypes = []wasm.TypeIdx{{}}; m.Code = []wasm.Func{{}} },
		"element":      func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"table-init":   func(m *wasm.Module) { m.Tables[0].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
		"table64":      func(m *wasm.Module) { m.Tables[0].Type.Limits.Addr64 = true },
		"missing-max":  func(m *wasm.Module) { m.Tables[0].Type.Limits.HasMax = false },
		"oversize":     func(m *wasm.Module) { m.Tables[0].Type.Limits.Max = MaxOwnedTableEntries + 1 },
		"bad-limits":   func(m *wasm.Module) { m.Tables[0].Type.Limits.Min = 6 },
		"extern-table": func(m *wasm.Module) { m.Tables[0].Type.Ref = wasm.AbsRef(wasm.HeapExtern) },
		"mutable":      func(m *wasm.Module) { m.Globals[0].Type.Mutable = true },
		"wrong-type":   func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.I64 },
		"derived":      func(m *wasm.Module) { m.Globals[1].Init.BodyBytes = []byte{0x23, 0, 0x0b} },
		"extended":     func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0x41, 1, 0x41, 2, 0x6a, 0x0b} },
		"v128":         func(m *wasm.Module) { m.Globals[3].Type.Type = wasm.V128 },
		"memory":       func(m *wasm.Module) { m.Memories = []wasm.MemType{{}} },
		"start":        func(m *wasm.Module) { start := wasm.FuncIdx(0); m.Start = &start },
		"missing-export": func(m *wasm.Module) {
			m.Exports = m.Exports[:4]
		},
		"duplicate-export": func(m *wasm.Module) {
			m.Exports[4].Index.Index = 2
		},
		"wrong-export": func(m *wasm.Module) {
			m.Exports[4].Index.Kind = wasm.ExternFunc
		},
		"duplicate-export-name": func(m *wasm.Module) {
			m.Exports[0].Name = m.Exports[1].Name
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := stateOnlyTable32ScalarGlobalsProfile()
			mutate(m)
			if admitStateOnlyTable32ScalarGlobals(m, true, true) {
				t.Fatal("unsupported table32/scalar-global composition admitted")
			}
			checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(false, true))
		})
	}
}

func TestStateOnlyTable32ScalarGlobalsGateDoesNotCaptureImportedFunctions(t *testing.T) {
	m := stateOnlyTable32ScalarGlobalsProfile()
	m.Types = []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}}
	m.Imports = []wasm.Import{{Module: "env", Name: "call", Type: wasm.NewFuncExternType(wasm.TypeIdx{})}}
	if admitStateOnlyTable32ScalarGlobals(m, true, true) {
		t.Fatal("imported-function module matched declaration-only profile")
	}
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("declaration-only gate captured imported-function table/global module", table, err)
	}
}
