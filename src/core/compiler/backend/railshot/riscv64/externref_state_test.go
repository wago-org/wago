//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func stateOnlyExternrefGlobalProfile(count int, exports ...int) *wasm.Module {
	m := &wasm.Module{Globals: make([]wasm.Global, count)}
	for i := range m.Globals {
		m.Globals[i] = wasm.Global{
			Type: wasm.GlobalType{Type: wasm.ExternRef},
			Init: wasm.Expr{BodyBytes: []byte{0xd0, 0x6f, 0x0b}},
		}
	}
	for _, index := range exports {
		m.Exports = append(m.Exports, wasm.Export{Name: fmt.Sprintf("g%d", index), Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: uint32(index)}})
	}
	return m
}

func TestStateOnlyExternrefGlobalProfile(t *testing.T) {
	for _, tc := range []struct {
		count   int
		exports []int
	}{{count: 1}, {count: 1, exports: []int{0}}, {count: 2, exports: []int{1}}, {count: 256, exports: []int{0, 255}}} {
		m := stateOnlyExternrefGlobalProfile(tc.count, tc.exports...)
		if !admitStateOnlyExternrefGlobals(m, true, true) {
			t.Fatal("exact state-only externref globals rejected", tc)
		}
		state, err := admitStatePolicy(m, true, false, true, false)
		if err != nil || len(state.globals) != tc.count {
			t.Fatal("state-only externref admission", tc, len(state.globals), err)
		}
		for _, global := range state.globals {
			if global.typ != wasm.ExternRef || !global.wide || global.mutable || global.value != 0 {
				t.Fatal("state-only externref metadata", global)
			}
		}
		if err := AdmitPublicInitializers(m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admitStateMode(stateOnlyExternrefGlobalProfile(2, 0, 1), true); err == nil {
		t.Fatal("private state-only externref global admitted")
	}
	if admitStateOnlyExternrefGlobals(stateOnlyExternrefGlobalProfile(257), true, true) {
		t.Fatal("257 state-only externref globals admitted")
	}
	exports := stateOnlyExternrefGlobalProfile(1)
	for i := range 257 {
		exports.Exports = append(exports.Exports, wasm.Export{Name: fmt.Sprintf("g%d", i), Index: wasm.ExternIdx{Kind: wasm.ExternGlobal}})
	}
	if admitStateOnlyExternrefGlobals(exports, true, true) {
		t.Fatal("257 state-only externref exports admitted")
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"mutable":      func(m *wasm.Module) { m.Globals[0].Type.Mutable = true },
		"wrong-type":   func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.FuncRef },
		"wrong-heap":   func(m *wasm.Module) { m.Globals[0].Init.BodyBytes[1] = 0x70 },
		"initializer":  func(m *wasm.Module) { m.Globals[0].Init.BodyBytes[0] = 0x23 },
		"table":        func(m *wasm.Module) { m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc)}}} },
		"wrong-export": func(m *wasm.Module) { m.Exports[0].Index.Kind = wasm.ExternFunc },
	} {
		t.Run(name, func(t *testing.T) {
			m := stateOnlyExternrefGlobalProfile(1, 0)
			mutate(m)
			if admitStateOnlyExternrefGlobals(m, true, true) {
				t.Fatal("unsupported state-only externref profile admitted")
			}
			if _, err := admitStatePolicy(m, true, false, true, false); err == nil {
				t.Fatal("unsupported state-only externref state admitted")
			}
		})
	}
}
