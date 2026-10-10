//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func stateOnlyTable64Profile() *wasm.Module {
	return &wasm.Module{
		Tables: []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 2, Max: 4, HasMax: true, Addr64: true}}}},
		Exports: []wasm.Export{
			{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
			{Name: "alias", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
		},
	}
}

func TestStateOnlyTable64Profile(t *testing.T) {
	m := stateOnlyTable64Profile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact state-only table64 profile rejected", table, err)
	}
	for _, tc := range []struct {
		production bool
		public     bool
	}{{false, true}, {true, false}, {false, false}} {
		if _, err := admitTableProfile(m, tc.production, tc.public); !errors.Is(err, ErrUnsupported) {
			t.Fatal("table64 profile escaped production/public boundary", tc, err)
		}
	}
}

func TestStateOnlyTable64ProfileRejectsExecutableAndResourceShapes(t *testing.T) {
	for name, mutate := range map[string]func(*wasm.Module){
		"import": func(m *wasm.Module) {
			m.Imports = []wasm.Import{{Module: "env", Name: "table", Type: wasm.NewTableExternType(m.Tables[0].Type)}}
			m.Tables = nil
		},
		"function":    func(m *wasm.Module) { m.FuncTypes = []wasm.TypeIdx{{Index: 0}}; m.Code = []wasm.Func{{}} },
		"element":     func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"initializer": func(m *wasm.Module) { m.Tables[0].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
		"start":       func(m *wasm.Module) { start := wasm.FuncIdx(0); m.Start = &start },
		"memory":      func(m *wasm.Module) { m.Memories = []wasm.MemType{{Limits: wasm.Limits{}}} },
		"global":      func(m *wasm.Module) { m.Globals = []wasm.Global{{}} },
		"tag":         func(m *wasm.Module) { m.Tags = []wasm.TagType{{}} },
		"data":        func(m *wasm.Module) { m.Data = []wasm.Data{{}} },
		"wrong-export": func(m *wasm.Module) {
			m.Exports[0].Index.Kind = wasm.ExternFunc
		},
		"externref": func(m *wasm.Module) { m.Tables[0].Type.Ref = wasm.AbsRef(wasm.HeapExtern) },
		"second":    func(m *wasm.Module) { m.Tables = append(m.Tables, m.Tables[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			m := stateOnlyTable64Profile()
			mutate(m)
			if _, err := admitTableProfile(m, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("unsupported table64 shape admitted", err)
			}
		})
	}
}

func ownedTable64SizeProfile() *wasm.Module {
	m := stateOnlyTable64Profile()
	m.Types = []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I64}}}}}}
	m.FuncTypes = []wasm.TypeIdx{{Index: 0}}
	m.Code = []wasm.Func{{BodyBytes: []byte{0xfc, 0x10, 0, 0x0b}}}
	m.Exports = append(m.Exports, wasm.Export{Name: "size", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}})
	return m
}

func TestOwnedTable64SizeProfile(t *testing.T) {
	m := ownedTable64SizeProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact table64.size profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":      func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x42, 0, 0x0b} },
		"result":    func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I32 },
		"param":     func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64} },
		"locals":    func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"no-export": func(m *wasm.Module) { m.Exports = m.Exports[:2] },
		"extra":     func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTable64SizeProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid table64.size profile admitted", err)
			}
		})
	}
}

func ownedTwoTable32SizeProfile() *wasm.Module {
	m := ownedTwoTable64SizeProfile()
	m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I32
	for i := range m.Tables {
		m.Tables[i].Type.Limits.Addr64 = false
	}
	return m
}

func TestOwnedTwoTable32SizeProfile(t *testing.T) {
	m := ownedTwoTable32SizeProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed two-table32.size profile rejected", table, err)
	}
	if err := AdmitPublicInitializers(m); err != nil {
		t.Fatal("two-table32 initializer preflight", err)
	}
	state, err := admitStatePolicy(m, true, false, true, true)
	if err != nil || len(state.tableAddr64) != 2 || state.tableAddr64[0] || state.tableAddr64[1] {
		t.Fatal("two-table32 state directory", state.tableAddr64, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":           func(m *wasm.Module) { m.Code[1].BodyBytes = []byte{0xfc, 0x10, 0, 0x0b} },
		"result":         func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"param":          func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I32} },
		"locals":         func(m *wasm.Module) { m.Code[1].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I32}} },
		"no-max":         func(m *wasm.Module) { m.Tables[1].Type.Limits.HasMax = false },
		"mixed-width":    func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = true },
		"externref":      func(m *wasm.Module) { m.Tables[1].Type.Ref = wasm.AbsRef(wasm.HeapExtern) },
		"no-export":      func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"extra-table":    func(m *wasm.Module) { m.Tables = append(m.Tables, m.Tables[1]) },
		"element":        func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"initializer":    func(m *wasm.Module) { m.Tables[1].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
		"branch-hints":   func(m *wasm.Module) { m.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0}} },
		"compact-import": func(m *wasm.Module) { m.UsesCompactImports = true },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable32SizeProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table32.size profile admitted", err)
			}
			if err := AdmitPublicInitializers(bad); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table32.size initializer preflight", err)
			}
		})
	}
	if _, err := admitTableProfile(m, true, false); !errors.Is(err, ErrUnsupported) {
		t.Fatal("indexed two-table32.size escaped retained public boundary", err)
	}
}

func ownedTwoTable32GetSetProfile() *wasm.Module {
	m := ownedTwoTable64GetSetProfile()
	m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32
	for i := range m.Tables {
		m.Tables[i].Type.Limits.Addr64 = false
	}
	return m
}

func TestOwnedTwoTable32GetSetProfile(t *testing.T) {
	m := ownedTwoTable32GetSetProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed two-table32 get/set profile rejected", table, err)
	}
	if err := AdmitPublicInitializers(m); err != nil {
		t.Fatal("two-table32 get/set initializer preflight", err)
	}
	state, err := admitStatePolicy(m, true, false, true, true)
	if err != nil || len(state.tableAddr64) != 2 || state.tableAddr64[0] || state.tableAddr64[1] {
		t.Fatal("two-table32 get/set state directory", state.tableAddr64, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body-index":     func(m *wasm.Module) { m.Code[1].BodyBytes[9] = 0 },
		"body-set":       func(m *wasm.Module) { m.Code[1].BodyBytes[5] = 0 },
		"result":         func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"param":          func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I64 },
		"locals":         func(m *wasm.Module) { m.Code[1].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I32}} },
		"no-max":         func(m *wasm.Module) { m.Tables[1].Type.Limits.HasMax = false },
		"mixed-width":    func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = true },
		"externref":      func(m *wasm.Module) { m.Tables[1].Type.Ref = wasm.AbsRef(wasm.HeapExtern) },
		"no-export":      func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"extra-table":    func(m *wasm.Module) { m.Tables = append(m.Tables, m.Tables[1]) },
		"element":        func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"initializer":    func(m *wasm.Module) { m.Tables[1].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
		"branch-hints":   func(m *wasm.Module) { m.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0}} },
		"compact-import": func(m *wasm.Module) { m.UsesCompactImports = true },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable32GetSetProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table32 get/set profile admitted", err)
			}
			if err := AdmitPublicInitializers(bad); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table32 get/set initializer preflight", err)
			}
		})
	}
	if _, err := admitTableProfile(m, true, false); !errors.Is(err, ErrUnsupported) {
		t.Fatal("indexed two-table32 get/set escaped retained public boundary", err)
	}
}

func ownedTwoTable32GrowProfile() *wasm.Module {
	m := ownedTwoTable64GrowProfile()
	m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32
	m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I32
	for i := range m.Tables {
		m.Tables[i].Type.Limits.Addr64 = false
	}
	return m
}

func TestOwnedTwoTable32GrowProfile(t *testing.T) {
	m := ownedTwoTable32GrowProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed two-table32 grow profile rejected", table, err)
	}
	if err := AdmitPublicInitializers(m); err != nil {
		t.Fatal("two-table32 grow initializer preflight", err)
	}
	state, err := admitStatePolicy(m, true, false, true, true)
	if err != nil || len(state.tableAddr64) != 2 || state.tableAddr64[0] || state.tableAddr64[1] {
		t.Fatal("two-table32 grow state directory", state.tableAddr64, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body-index":     func(m *wasm.Module) { m.Code[1].BodyBytes[6] = 0 },
		"result":         func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"param":          func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I64 },
		"locals":         func(m *wasm.Module) { m.Code[1].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I32}} },
		"no-max":         func(m *wasm.Module) { m.Tables[1].Type.Limits.HasMax = false },
		"mixed-width":    func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = true },
		"externref":      func(m *wasm.Module) { m.Tables[1].Type.Ref = wasm.AbsRef(wasm.HeapExtern) },
		"no-export":      func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"extra-table":    func(m *wasm.Module) { m.Tables = append(m.Tables, m.Tables[1]) },
		"element":        func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"initializer":    func(m *wasm.Module) { m.Tables[1].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
		"branch-hints":   func(m *wasm.Module) { m.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0}} },
		"compact-import": func(m *wasm.Module) { m.UsesCompactImports = true },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable32GrowProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table32 grow profile admitted", err)
			}
			if err := AdmitPublicInitializers(bad); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table32 grow initializer preflight", err)
			}
		})
	}
	if _, err := admitTableProfile(m, true, false); !errors.Is(err, ErrUnsupported) {
		t.Fatal("indexed two-table32 grow escaped retained public boundary", err)
	}
}

func ownedTwoTable32FillProfile() *wasm.Module {
	m := ownedTwoTable64FillProfile()
	m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32
	m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I32
	for i := range m.Tables {
		m.Tables[i].Type.Limits.Addr64 = false
	}
	return m
}

func TestOwnedTwoTable32FillProfile(t *testing.T) {
	m := ownedTwoTable32FillProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed two-table32 fill profile rejected", table, err)
	}
	if err := AdmitPublicInitializers(m); err != nil {
		t.Fatal("two-table32 fill initializer preflight", err)
	}
	state, err := admitStatePolicy(m, true, false, true, true)
	if err != nil || len(state.tableAddr64) != 2 || state.tableAddr64[0] || state.tableAddr64[1] {
		t.Fatal("two-table32 fill state directory", state.tableAddr64, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body-index":     func(m *wasm.Module) { m.Code[1].BodyBytes[8] = 0 },
		"result":         func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"first-param":    func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I64 },
		"second-param":   func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I64 },
		"locals":         func(m *wasm.Module) { m.Code[1].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I32}} },
		"no-max":         func(m *wasm.Module) { m.Tables[1].Type.Limits.HasMax = false },
		"mixed-width":    func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = true },
		"externref":      func(m *wasm.Module) { m.Tables[1].Type.Ref = wasm.AbsRef(wasm.HeapExtern) },
		"no-export":      func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"extra-table":    func(m *wasm.Module) { m.Tables = append(m.Tables, m.Tables[1]) },
		"element":        func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"initializer":    func(m *wasm.Module) { m.Tables[1].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
		"branch-hints":   func(m *wasm.Module) { m.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0}} },
		"compact-import": func(m *wasm.Module) { m.UsesCompactImports = true },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable32FillProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table32 fill profile admitted", err)
			}
			if err := AdmitPublicInitializers(bad); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table32 fill initializer preflight", err)
			}
		})
	}
	if _, err := admitTableProfile(m, true, false); !errors.Is(err, ErrUnsupported) {
		t.Fatal("indexed two-table32 fill escaped retained public boundary", err)
	}
}

func ownedTwoTable64SizeProfile() *wasm.Module {
	return &wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I64}}}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 0}},
		Code: []wasm.Func{
			{BodyBytes: []byte{0xfc, 0x10, 0, 0x0b}},
			{BodyBytes: []byte{0xfc, 0x10, 1, 0x0b}},
		},
		Tables: []wasm.Table{
			{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 1, Max: 2, HasMax: true, Addr64: true}}},
			{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 3, Max: 4, HasMax: true, Addr64: true}}},
		},
		Exports: []wasm.Export{
			{Name: "size0", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}},
			{Name: "size1", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}},
			{Name: "table0", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
			{Name: "table1", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 1}},
		},
	}
}

func TestOwnedTwoTable64SizeProfile(t *testing.T) {
	m := ownedTwoTable64SizeProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed two-table64.size profile rejected", table, err)
	}
	state, err := admitStatePolicy(m, true, false, true, true)
	if err != nil || len(state.tableAddr64) != 2 || !state.tableAddr64[0] || !state.tableAddr64[1] {
		t.Fatal("two-table64 state directory", state.tableAddr64, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":           func(m *wasm.Module) { m.Code[1].BodyBytes = []byte{0xfc, 0x10, 0, 0x0b} },
		"result":         func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I32 },
		"param":          func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64} },
		"locals":         func(m *wasm.Module) { m.Code[1].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"no-max":         func(m *wasm.Module) { m.Tables[1].Type.Limits.HasMax = false },
		"table32":        func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = false },
		"externref":      func(m *wasm.Module) { m.Tables[1].Type.Ref = wasm.AbsRef(wasm.HeapExtern) },
		"no-export":      func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"extra-table":    func(m *wasm.Module) { m.Tables = append(m.Tables, m.Tables[1]) },
		"element":        func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"initializer":    func(m *wasm.Module) { m.Tables[1].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
		"branch-hints":   func(m *wasm.Module) { m.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0}} },
		"compact-import": func(m *wasm.Module) { m.UsesCompactImports = true },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable64SizeProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table64.size profile admitted", err)
			}
		})
	}
	if _, err := admitTableProfile(m, true, false); !errors.Is(err, ErrUnsupported) {
		t.Fatal("indexed two-table64.size escaped retained public boundary", err)
	}
}

func ownedTwoTable64GetSetProfile() *wasm.Module {
	m := ownedTwoTable64SizeProfile()
	m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64}
	m.Types[0].SubTypes[0].Comp.Results = []wasm.ValType{wasm.I32}
	m.Code[0].BodyBytes = []byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0x20, 0, 0x25, 0, 0xd1, 0x0b}
	m.Code[1].BodyBytes = []byte{0x20, 0, 0xd0, 0x70, 0x26, 1, 0x20, 0, 0x25, 1, 0xd1, 0x0b}
	m.Exports[0].Name, m.Exports[1].Name = "roundtrip0", "roundtrip1"
	return m
}

func TestOwnedTwoTable64GetSetProfile(t *testing.T) {
	m := ownedTwoTable64GetSetProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed two-table64 get/set profile rejected", table, err)
	}
	state, err := admitStatePolicy(m, true, false, true, true)
	if err != nil || len(state.tableAddr64) != 2 || !state.tableAddr64[0] || !state.tableAddr64[1] {
		t.Fatal("two-table64 get/set state directory", state.tableAddr64, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body-index":  func(m *wasm.Module) { m.Code[1].BodyBytes[9] = 0 },
		"body-set":    func(m *wasm.Module) { m.Code[1].BodyBytes[5] = 0 },
		"result":      func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"param":       func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"no-max":      func(m *wasm.Module) { m.Tables[1].Type.Limits.HasMax = false },
		"table32":     func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = false },
		"no-export":   func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"element":     func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"initializer": func(m *wasm.Module) { m.Tables[1].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable64GetSetProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table64 get/set profile admitted", err)
			}
		})
	}
}

func ownedTwoTable64GrowProfile() *wasm.Module {
	m := ownedTwoTable64SizeProfile()
	m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64}
	m.Code[0].BodyBytes = []byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, 0, 0x0b}
	m.Code[1].BodyBytes = []byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, 1, 0x0b}
	m.Exports[0].Name, m.Exports[1].Name = "grow0", "grow1"
	return m
}

func TestOwnedTwoTable64GrowProfile(t *testing.T) {
	m := ownedTwoTable64GrowProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed two-table64 grow profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body-index":  func(m *wasm.Module) { m.Code[1].BodyBytes[6] = 0 },
		"result":      func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I32 },
		"param":       func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"no-max":      func(m *wasm.Module) { m.Tables[1].Type.Limits.HasMax = false },
		"table32":     func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = false },
		"no-export":   func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"element":     func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"initializer": func(m *wasm.Module) { m.Tables[1].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable64GrowProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table64 grow profile admitted", err)
			}
		})
	}
}

func ownedTwoTable64FillProfile() *wasm.Module {
	m := ownedTwoTable64SizeProfile()
	m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64, wasm.I64}
	m.Types[0].SubTypes[0].Comp.Results = []wasm.ValType{wasm.I32}
	m.Code[0].BodyBytes = []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, 0, 0x41, 1, 0x0b}
	m.Code[1].BodyBytes = []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, 1, 0x41, 1, 0x0b}
	m.Exports[0].Name, m.Exports[1].Name = "fill0", "fill1"
	return m
}

func TestOwnedTwoTable64FillProfile(t *testing.T) {
	m := ownedTwoTable64FillProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed two-table64 fill profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body-index":  func(m *wasm.Module) { m.Code[1].BodyBytes[8] = 0 },
		"result":      func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"first-param": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"count-param": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I32 },
		"no-max":      func(m *wasm.Module) { m.Tables[1].Type.Limits.HasMax = false },
		"table32":     func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = false },
		"no-export":   func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"element":     func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"initializer": func(m *wasm.Module) { m.Tables[1].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable64FillProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table64 fill profile admitted", err)
			}
		})
	}
}

func ownedTwoTable64CopyProfile() *wasm.Module {
	m := ownedTwoTable64SizeProfile()
	m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64, wasm.I64, wasm.I64}
	m.Types[0].SubTypes[0].Comp.Results = []wasm.ValType{wasm.I32}
	m.Code[0].BodyBytes = []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, 0, 0, 0x41, 1, 0x0b}
	m.Code[1].BodyBytes = []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, 1, 1, 0x41, 1, 0x0b}
	m.Exports[0].Name, m.Exports[1].Name = "copy0", "copy1"
	return m
}

func TestOwnedTwoTable64CopyProfile(t *testing.T) {
	m := ownedTwoTable64CopyProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed two-table64 copy profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"destination-index": func(m *wasm.Module) { m.Code[1].BodyBytes[8] = 0 },
		"source-index":      func(m *wasm.Module) { m.Code[1].BodyBytes[9] = 0 },
		"result":            func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"first-param":       func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"source-param":      func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I32 },
		"count-param":       func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[2] = wasm.I32 },
		"no-max":            func(m *wasm.Module) { m.Tables[1].Type.Limits.HasMax = false },
		"table32":           func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = false },
		"no-export":         func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"element":           func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"initializer":       func(m *wasm.Module) { m.Tables[1].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable64CopyProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed two-table64 copy profile admitted", err)
			}
		})
	}
}

func ownedTwoTable64CrossCopyProfile() *wasm.Module {
	m := ownedTwoTable64CopyProfile()
	m.Code[0].BodyBytes[9] = 1
	m.Code[1].BodyBytes[9] = 0
	m.Exports[0].Name, m.Exports[1].Name = "copy01", "copy10"
	return m
}

func TestOwnedTwoTable64CrossCopyProfile(t *testing.T) {
	m := ownedTwoTable64CrossCopyProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact cross-table64 copy profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"same-table":   func(m *wasm.Module) { m.Code[1].BodyBytes[9] = 1 },
		"bad-dest":     func(m *wasm.Module) { m.Code[1].BodyBytes[8] = 0 },
		"result":       func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"source-param": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I32 },
		"no-max":       func(m *wasm.Module) { m.Tables[1].Type.Limits.HasMax = false },
		"table32":      func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = false },
		"no-export":    func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"element":      func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable64CrossCopyProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid cross-table64 copy profile admitted", err)
			}
		})
	}
}

func importedTable64SizeProfile() *wasm.Module {
	table := wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 1, Max: 4, HasMax: true, Addr64: true}}
	return &wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I64}}}}}},
		Imports:   []wasm.Import{{Module: "env", Name: "table", Type: wasm.NewTableExternType(table)}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Code:      []wasm.Func{{BodyBytes: []byte{0xfc, 0x10, 0, 0x0b}}},
		Exports: []wasm.Export{
			{Name: "size", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}},
			{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
		},
	}
}

func TestImportedTable64SizeProfile(t *testing.T) {
	m := importedTable64SizeProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact imported table64.size profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":   func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x42, 0, 0x0b} },
		"result": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I32 },
		"param":  func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64} },
		"no-max": func(m *wasm.Module) {
			t := m.Imports[0].Type.TableType()
			t.Limits.HasMax = false
			m.Imports[0].Type = wasm.NewTableExternType(t)
		},
		"locals":    func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"no-export": func(m *wasm.Module) { m.Exports = m.Exports[1:] },
		"extra":     func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := importedTable64SizeProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid imported table64.size profile admitted", err)
			}
		})
	}
	if _, err := admitTableProfile(m, true, false); !errors.Is(err, ErrUnsupported) {
		t.Fatal("imported table64.size escaped retained public table ABI", err)
	}
}

func importedTable64GetSetProfile() *wasm.Module {
	m := importedTable64SizeProfile()
	m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64}
	m.Types[0].SubTypes[0].Comp.Results = []wasm.ValType{wasm.I32}
	m.Code[0].BodyBytes = []byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0x20, 0, 0x25, 0, 0xd1, 0x0b}
	m.Exports[0].Name = "roundtrip"
	return m
}

func TestImportedTable64GetSetProfile(t *testing.T) {
	m := importedTable64GetSetProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact imported table64.get/set profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":   func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x41, 1, 0x0b} },
		"result": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"param":  func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"no-max": func(m *wasm.Module) {
			t := m.Imports[0].Type.TableType()
			t.Limits.HasMax = false
			m.Imports[0].Type = wasm.NewTableExternType(t)
		},
		"locals":    func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"no-export": func(m *wasm.Module) { m.Exports = m.Exports[1:] },
		"extra":     func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := importedTable64GetSetProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid imported table64.get/set profile admitted", err)
			}
		})
	}
}

func importedTable64GrowProfile() *wasm.Module {
	m := importedTable64SizeProfile()
	m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64}
	m.Code[0].BodyBytes = []byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, 0, 0x0b}
	m.Exports[0].Name = "grow"
	return m
}

func TestImportedTable64GrowProfile(t *testing.T) {
	m := importedTable64GrowProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact imported table64.grow profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":   func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x42, 0, 0x0b} },
		"result": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I32 },
		"param":  func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"no-max": func(m *wasm.Module) {
			t := m.Imports[0].Type.TableType()
			t.Limits.HasMax = false
			m.Imports[0].Type = wasm.NewTableExternType(t)
		},
		"locals":    func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"no-export": func(m *wasm.Module) { m.Exports = m.Exports[1:] },
		"extra":     func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := importedTable64GrowProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid imported table64.grow profile admitted", err)
			}
		})
	}
}

func importedTable64FillProfile() *wasm.Module {
	m := importedTable64SizeProfile()
	m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64, wasm.I64}
	m.Types[0].SubTypes[0].Comp.Results = []wasm.ValType{wasm.I32}
	m.Code[0].BodyBytes = []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, 0, 0x41, 1, 0x0b}
	m.Exports[0].Name = "fill"
	return m
}

func TestImportedTable64FillProfile(t *testing.T) {
	m := importedTable64FillProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact imported table64.fill profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":   func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x41, 1, 0x0b} },
		"result": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"first-param": func(m *wasm.Module) {
			m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32
		},
		"second-param": func(m *wasm.Module) {
			m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I32
		},
		"no-max": func(m *wasm.Module) {
			t := m.Imports[0].Type.TableType()
			t.Limits.HasMax = false
			m.Imports[0].Type = wasm.NewTableExternType(t)
		},
		"locals":    func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"no-export": func(m *wasm.Module) { m.Exports = m.Exports[1:] },
		"extra":     func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := importedTable64FillProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid imported table64.fill profile admitted", err)
			}
		})
	}
}

func importedTable64CopyProfile() *wasm.Module {
	m := importedTable64SizeProfile()
	m.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I64, wasm.I64, wasm.I64}
	m.Types[0].SubTypes[0].Comp.Results = []wasm.ValType{wasm.I32}
	m.Code[0].BodyBytes = []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, 0, 0, 0x41, 1, 0x0b}
	m.Exports[0].Name = "copy"
	return m
}

func TestImportedTable64CopyProfile(t *testing.T) {
	m := importedTable64CopyProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact imported table64.copy profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":   func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x41, 1, 0x0b} },
		"result": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"first-param": func(m *wasm.Module) {
			m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32
		},
		"second-param": func(m *wasm.Module) {
			m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I32
		},
		"third-param": func(m *wasm.Module) {
			m.Types[0].SubTypes[0].Comp.Params[2] = wasm.I32
		},
		"no-max": func(m *wasm.Module) {
			t := m.Imports[0].Type.TableType()
			t.Limits.HasMax = false
			m.Imports[0].Type = wasm.NewTableExternType(t)
		},
		"locals":    func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"no-export": func(m *wasm.Module) { m.Exports = m.Exports[1:] },
		"extra":     func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := importedTable64CopyProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid imported table64.copy profile admitted", err)
			}
		})
	}
}

func ownedTable64GrowProfile() *wasm.Module {
	m := stateOnlyTable64Profile()
	m.Types = []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I64}}}}}}
	m.FuncTypes = []wasm.TypeIdx{{Index: 0}}
	m.Code = []wasm.Func{{BodyBytes: []byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, 0, 0x0b}}}
	m.Exports = append(m.Exports, wasm.Export{Name: "grow", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}})
	return m
}

func TestOwnedTable64GrowProfile(t *testing.T) {
	m := ownedTable64GrowProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact table64.grow profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":      func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x42, 0, 0x0b} },
		"result":    func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I32 },
		"param":     func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"no-max":    func(m *wasm.Module) { m.Tables[0].Type.Limits.HasMax = false },
		"locals":    func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"no-export": func(m *wasm.Module) { m.Exports = m.Exports[:2] },
		"extra":     func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTable64GrowProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid table64.grow profile admitted", err)
			}
		})
	}
}

func ownedTable64GetSetProfile() *wasm.Module {
	m := stateOnlyTable64Profile()
	m.Types = []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I32}}}}}}
	m.FuncTypes = []wasm.TypeIdx{{Index: 0}}
	m.Code = []wasm.Func{{BodyBytes: []byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0x20, 0, 0x25, 0, 0xd1, 0x0b}}}
	m.Exports = append(m.Exports, wasm.Export{Name: "roundtrip", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}})
	return m
}

func TestOwnedTable64GetSetProfile(t *testing.T) {
	m := ownedTable64GetSetProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact table64 get/set profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":      func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x42, 0, 0x0b} },
		"result":    func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"param":     func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"no-max":    func(m *wasm.Module) { m.Tables[0].Type.Limits.HasMax = false },
		"locals":    func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"no-export": func(m *wasm.Module) { m.Exports = m.Exports[:2] },
		"extra":     func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTable64GetSetProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid table64 get/set profile admitted", err)
			}
		})
	}
}

func ownedTable64IndirectProfile() *wasm.Module {
	m := stateOnlyTable64Profile()
	m.Types = []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}}}},
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I32}}}}},
	}
	m.FuncTypes = []wasm.TypeIdx{{Index: 0}, {Index: 1}}
	m.Code = []wasm.Func{
		{BodyBytes: []byte{0x41, 7, 0x0b}},
		{BodyBytes: []byte{0x20, 0, 0xd2, 0, 0x26, 0, 0x20, 0, 0x11, 0, 0, 0x0b}},
	}
	m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}}
	m.Exports = append(m.Exports, wasm.Export{Name: "call", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}})
	return m
}

func TestOwnedTable64IndirectProfile(t *testing.T) {
	m := ownedTable64IndirectProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact table64 indirect-call profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"target-body":   func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x41, 8, 0x0b} },
		"caller-body":   func(m *wasm.Module) { m.Code[1].BodyBytes = []byte{0x20, 0, 0x1a, 0x41, 7, 0x0b} },
		"caller-param":  func(m *wasm.Module) { m.Types[1].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"target-result": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"active-element": func(m *wasm.Module) {
			m.Elements[0].Mode.Kind = wasm.ElemActive
		},
		"wrong-declaration": func(m *wasm.Module) { m.Elements[0].Kind.Funcs[0] = 1 },
		"no-max":            func(m *wasm.Module) { m.Tables[0].Type.Limits.HasMax = false },
		"locals":            func(m *wasm.Module) { m.Code[1].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"no-export":         func(m *wasm.Module) { m.Exports = m.Exports[:2] },
		"extra":             func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTable64IndirectProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid table64 indirect-call profile admitted", err)
			}
		})
	}
}

func ownedTwoTable64IndirectProfile() *wasm.Module {
	m := ownedTable64IndirectProfile()
	m.Tables = append(m.Tables, m.Tables[0])
	m.FuncTypes = []wasm.TypeIdx{{Index: 0}, {Index: 1}, {Index: 1}}
	m.Code = []wasm.Func{
		{BodyBytes: []byte{0x41, 7, 0x0b}},
		{BodyBytes: []byte{0x20, 0, 0xd2, 0, 0x26, 0, 0x20, 0, 0x11, 0, 0, 0x0b}},
		{BodyBytes: []byte{0x20, 0, 0xd2, 0, 0x26, 1, 0x20, 0, 0x11, 0, 1, 0x0b}},
	}
	m.Exports = []wasm.Export{
		{Name: "call0", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}},
		{Name: "call1", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}},
		{Name: "table0", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
		{Name: "table1", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 1}},
	}
	return m
}

func TestOwnedTwoTable64IndirectProfile(t *testing.T) {
	m := ownedTwoTable64IndirectProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed table64 indirect-call profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"first-index":    func(m *wasm.Module) { m.Code[1].BodyBytes[5] = 1 },
		"second-set":     func(m *wasm.Module) { m.Code[2].BodyBytes[5] = 0 },
		"second-call":    func(m *wasm.Module) { m.Code[2].BodyBytes[10] = 0 },
		"second-table32": func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = false },
		"missing-export": func(m *wasm.Module) { m.Exports = m.Exports[:3] },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable64IndirectProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed table64 indirect-call profile admitted", err)
			}
		})
	}
}

func importedTable64IndirectProfile() *wasm.Module {
	m := importedTable64SizeProfile()
	m.Types = []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}}}},
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I32}}}}},
	}
	m.FuncTypes = []wasm.TypeIdx{{Index: 1}}
	m.Code = []wasm.Func{{BodyBytes: []byte{0x20, 0, 0x11, 0, 0, 0x0b}}}
	m.Exports[0].Name = "call"
	return m
}

func TestImportedTable64IndirectProfile(t *testing.T) {
	m := importedTable64IndirectProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact imported table64 indirect-call profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":          func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x20, 0, 0x1a, 0x41, 7, 0x0b} },
		"caller-param":  func(m *wasm.Module) { m.Types[1].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"target-result": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"wrong-type":    func(m *wasm.Module) { m.FuncTypes[0].Index = 0 },
		"no-max": func(m *wasm.Module) {
			t := m.Imports[0].Type.TableType()
			t.Limits.HasMax = false
			m.Imports[0].Type = wasm.NewTableExternType(t)
		},
		"locals":    func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"element":   func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}}} },
		"no-export": func(m *wasm.Module) { m.Exports = m.Exports[1:] },
		"extra":     func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := importedTable64IndirectProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid imported table64 indirect-call profile admitted", err)
			}
		})
	}
	if _, err := admitTableProfile(m, true, false); !errors.Is(err, ErrUnsupported) {
		t.Fatal("imported table64 indirect call escaped retained public table ABI", err)
	}
}

func ownedTable64FillProfile() *wasm.Module {
	m := stateOnlyTable64Profile()
	m.Types = []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64, wasm.I64}, Results: []wasm.ValType{wasm.I32}}}}}}
	m.FuncTypes = []wasm.TypeIdx{{Index: 0}}
	m.Code = []wasm.Func{{BodyBytes: []byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 0x11, 0, 0x41, 1, 0x0b}}}
	m.Exports = append(m.Exports, wasm.Export{Name: "fill", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}})
	return m
}

func TestOwnedTable64FillProfile(t *testing.T) {
	m := ownedTable64FillProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact table64 fill profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":         func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x41, 1, 0x0b} },
		"first-param":  func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"second-param": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I32 },
		"result":       func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"no-max":       func(m *wasm.Module) { m.Tables[0].Type.Limits.HasMax = false },
		"locals":       func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"element":      func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"no-export":    func(m *wasm.Module) { m.Exports = m.Exports[:2] },
		"extra":        func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTable64FillProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid table64.fill profile admitted", err)
			}
		})
	}
}

func ownedTable64CopyProfile() *wasm.Module {
	m := stateOnlyTable64Profile()
	m.Types = []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, Results: []wasm.ValType{wasm.I32}}}}}}
	m.FuncTypes = []wasm.TypeIdx{{Index: 0}}
	m.Code = []wasm.Func{{BodyBytes: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0e, 0, 0, 0x41, 1, 0x0b}}}
	m.Exports = append(m.Exports, wasm.Export{Name: "copy", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}})
	return m
}

func TestOwnedTable64CopyProfile(t *testing.T) {
	m := ownedTable64CopyProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact table64.copy profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":         func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x41, 1, 0x0b} },
		"first-param":  func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"second-param": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I32 },
		"third-param":  func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[2] = wasm.I32 },
		"result":       func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"no-max":       func(m *wasm.Module) { m.Tables[0].Type.Limits.HasMax = false },
		"locals":       func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"element":      func(m *wasm.Module) { m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}}} },
		"no-export":    func(m *wasm.Module) { m.Exports = m.Exports[:2] },
		"extra":        func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTable64CopyProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid table64.copy profile admitted", err)
			}
		})
	}
}

func ownedTable64InitDropProfile() *wasm.Module {
	m := stateOnlyTable64Profile()
	m.Types = []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}},
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}}}},
	}
	m.FuncTypes = []wasm.TypeIdx{{Index: 0}, {Index: 1}}
	m.Code = []wasm.Func{
		{BodyBytes: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0c, 0, 0, 0x41, 1, 0x0b}},
		{BodyBytes: []byte{0xfc, 0x0d, 0, 0x41, 1, 0x0b}},
	}
	m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemTypedExprs, Ref: wasm.AbsRef(wasm.HeapFunc), Exprs: []wasm.Expr{{BodyBytes: []byte{0xd0, 0x70, 0x0b}}}}}}
	m.Exports = append(m.Exports,
		wasm.Export{Name: "init", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}},
		wasm.Export{Name: "drop", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}},
	)
	return m
}

func TestOwnedTable64InitDropProfile(t *testing.T) {
	m := ownedTable64InitDropProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact table64.init/drop profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"init-body":  func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x41, 1, 0x0b} },
		"drop-body":  func(m *wasm.Module) { m.Code[1].BodyBytes = []byte{0x41, 1, 0x0b} },
		"dest-param": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"source-param": func(m *wasm.Module) {
			m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I64
		},
		"count-param": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[2] = wasm.I64 },
		"drop-result": func(m *wasm.Module) { m.Types[1].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"no-max":      func(m *wasm.Module) { m.Tables[0].Type.Limits.HasMax = false },
		"locals":      func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"active":      func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemActive },
		"nonnull": func(m *wasm.Module) {
			m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd2, 0, 0x0b}
		},
		"missing-drop-export": func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"extra":               func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTable64InitDropProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid table64.init/drop profile admitted", err)
			}
		})
	}
}

func ownedTwoTable64InitDropProfile() *wasm.Module {
	m := ownedTable64InitDropProfile()
	m.Tables = append(m.Tables, m.Tables[0])
	m.FuncTypes = []wasm.TypeIdx{{Index: 0}, {Index: 0}, {Index: 1}}
	m.Code = []wasm.Func{
		{BodyBytes: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0c, 0, 0, 0x41, 1, 0x0b}},
		{BodyBytes: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0c, 0, 1, 0x41, 1, 0x0b}},
		{BodyBytes: []byte{0xfc, 0x0d, 0, 0x41, 1, 0x0b}},
	}
	m.Exports = []wasm.Export{
		{Name: "init0", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}},
		{Name: "init1", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}},
		{Name: "drop", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}},
		{Name: "table0", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
		{Name: "table1", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 1}},
	}
	return m
}

func TestOwnedTwoTable64InitDropProfile(t *testing.T) {
	m := ownedTwoTable64InitDropProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact indexed table64.init/drop profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"first-index":    func(m *wasm.Module) { m.Code[0].BodyBytes[9] = 1 },
		"second-index":   func(m *wasm.Module) { m.Code[1].BodyBytes[9] = 0 },
		"second-table32": func(m *wasm.Module) { m.Tables[1].Type.Limits.Addr64 = false },
		"missing-export": func(m *wasm.Module) { m.Exports = m.Exports[:4] },
		"extra-element":  func(m *wasm.Module) { m.Elements = append(m.Elements, m.Elements[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ownedTwoTable64InitDropProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid indexed table64.init/drop profile admitted", err)
			}
		})
	}
}

func importedTable64InitDropProfile() *wasm.Module {
	m := ownedTable64InitDropProfile()
	table := m.Tables[0].Type
	table.Limits.Min, table.Limits.Max = 1, 4
	m.Tables = nil
	m.Imports = []wasm.Import{{Module: "env", Name: "table", Type: wasm.NewTableExternType(table)}}
	return m
}

func TestImportedTable64InitDropProfile(t *testing.T) {
	m := importedTable64InitDropProfile()
	if table, err := admitTableProfile(m, true, true); err != nil || !table {
		t.Fatal("exact imported table64.init/drop profile rejected", table, err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"init-body":  func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x41, 1, 0x0b} },
		"drop-body":  func(m *wasm.Module) { m.Code[1].BodyBytes = []byte{0x41, 1, 0x0b} },
		"dest-param": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[0] = wasm.I32 },
		"source-param": func(m *wasm.Module) {
			m.Types[0].SubTypes[0].Comp.Params[1] = wasm.I64
		},
		"count-param": func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params[2] = wasm.I64 },
		"drop-result": func(m *wasm.Module) { m.Types[1].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"no-max": func(m *wasm.Module) {
			t := m.Imports[0].Type.TableType()
			t.Limits.HasMax = false
			m.Imports[0].Type = wasm.NewTableExternType(t)
		},
		"locals": func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I64}} },
		"active": func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemActive },
		"nonnull": func(m *wasm.Module) {
			m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd2, 0, 0x0b}
		},
		"missing-drop-export": func(m *wasm.Module) { m.Exports = m.Exports[:3] },
		"extra":               func(m *wasm.Module) { m.Code = append(m.Code, wasm.Func{}) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := importedTable64InitDropProfile()
			mutate(bad)
			if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid imported table64.init/drop profile admitted", err)
			}
		})
	}
}
