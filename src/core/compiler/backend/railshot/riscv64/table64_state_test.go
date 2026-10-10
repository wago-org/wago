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
