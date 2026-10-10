//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
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

func stateOnlyExternrefGlobalChainProfile(parents ...int) *wasm.Module {
	m := stateOnlyExternrefGlobalProfile(len(parents))
	for i, parent := range parents {
		if parent < 0 {
			continue
		}
		m.Globals[i].Init.BodyBytes = append([]byte{0x23}, wasmtest.ULEB(uint32(parent))...)
		m.Globals[i].Init.BodyBytes = append(m.Globals[i].Init.BodyBytes, 0x0b)
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

func TestStateOnlyExternrefGlobalChainProfile(t *testing.T) {
	maximum := make([]int, 256)
	maximum[0] = -1
	for i := 1; i < len(maximum); i++ {
		maximum[i] = i - 1
	}
	for _, parents := range [][]int{{-1, 0}, {-1, -1, 0, 0, 2}, maximum} {
		m := stateOnlyExternrefGlobalChainProfile(parents...)
		if !admitStateOnlyExternrefGlobals(m, true, true) {
			t.Fatal("exact state-only externref global chain rejected", parents)
		}
		state, err := admitStatePolicy(m, true, false, true, false)
		if err != nil || len(state.globals) != len(parents) {
			t.Fatal("state-only externref chain admission", parents, len(state.globals), err)
		}
		if err := AdmitPublicInitializers(m); err != nil {
			t.Fatal(err)
		}
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"self": func(m *wasm.Module) {
			m.Globals[1].Init.BodyBytes = []byte{0x23, 1, 0x0b}
		},
		"forward": func(m *wasm.Module) {
			m.Globals[1].Init.BodyBytes = []byte{0x23, 2, 0x0b}
		},
		"mutable-predecessor": func(m *wasm.Module) {
			m.Globals[0].Type.Mutable = true
		},
		"wrong-type-predecessor": func(m *wasm.Module) {
			m.Globals[0].Type.Type = wasm.FuncRef
		},
		"trailing-expression": func(m *wasm.Module) {
			m.Globals[1].Init.BodyBytes = []byte{0x23, 0, 0xd1, 0x0b}
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := stateOnlyExternrefGlobalChainProfile(-1, 0, 1)
			mutate(m)
			if admitStateOnlyExternrefGlobals(m, true, true) {
				t.Fatal("unsupported state-only externref chain admitted")
			}
			if _, err := admitStatePolicy(m, true, false, true, false); err == nil {
				t.Fatal("unsupported state-only externref chain state admitted")
			}
		})
	}
}
