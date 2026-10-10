//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func stateOnlyExternrefGlobalProfile(export bool) *wasm.Module {
	m := &wasm.Module{Globals: []wasm.Global{{
		Type: wasm.GlobalType{Type: wasm.ExternRef},
		Init: wasm.Expr{BodyBytes: []byte{0xd0, 0x6f, 0x0b}},
	}}}
	if export {
		m.Exports = []wasm.Export{{Name: "g", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal}}}
	}
	return m
}

func TestStateOnlyExternrefGlobalProfile(t *testing.T) {
	for _, export := range []bool{false, true} {
		m := stateOnlyExternrefGlobalProfile(export)
		if !admitStateOnlyExternrefGlobal(m, true, true) {
			t.Fatal("exact state-only externref global rejected", export)
		}
		state, err := admitStatePolicy(m, true, false, true, false)
		if err != nil || len(state.globals) != 1 || state.globals[0].typ != wasm.ExternRef || !state.globals[0].wide || state.globals[0].mutable || state.globals[0].value != 0 {
			t.Fatal("state-only externref admission", export, state.globals, err)
		}
		if err := AdmitPublicInitializers(m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admitStateMode(stateOnlyExternrefGlobalProfile(true), true); err == nil {
		t.Fatal("private state-only externref global admitted")
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"mutable":       func(m *wasm.Module) { m.Globals[0].Type.Mutable = true },
		"wrong-type":    func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.FuncRef },
		"wrong-heap":    func(m *wasm.Module) { m.Globals[0].Init.BodyBytes[1] = 0x70 },
		"initializer":   func(m *wasm.Module) { m.Globals[0].Init.BodyBytes[0] = 0x23 },
		"extra-global":  func(m *wasm.Module) { m.Globals = append(m.Globals, m.Globals[0]) },
		"table":         func(m *wasm.Module) { m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc)}}} },
		"wrong-export":  func(m *wasm.Module) { m.Exports[0].Index.Kind = wasm.ExternFunc },
		"second-export": func(m *wasm.Module) { m.Exports = append(m.Exports, m.Exports[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			m := stateOnlyExternrefGlobalProfile(true)
			mutate(m)
			if admitStateOnlyExternrefGlobal(m, true, true) {
				t.Fatal("unsupported state-only externref profile admitted")
			}
			if _, err := admitStatePolicy(m, true, false, true, false); err == nil {
				t.Fatal("unsupported state-only externref state admitted")
			}
		})
	}
}
