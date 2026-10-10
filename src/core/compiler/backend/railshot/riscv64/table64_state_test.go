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
