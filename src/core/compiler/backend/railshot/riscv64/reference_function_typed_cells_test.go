//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func privateIndexedFunctionGlobalFixture() *wasm.Module {
	function := func(params, results []wasm.ValType) wasm.RecType {
		return wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: results}}}}
	}
	m := &wasm.Module{
		Types:     []wasm.RecType{function([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}), function(nil, nil)},
		FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 1}},
		Code:      make([]wasm.Func, 2),
	}
	m.Globals = []wasm.Global{{
		Type: wasm.GlobalType{Type: wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))},
		Init: wasm.Expr{BodyBytes: []byte{0xd2, 0, 0x0b}},
	}}
	return m
}

func TestPrivateIndexedFunctionGlobalConfinement(t *testing.T) {
	m := privateIndexedFunctionGlobalFixture()
	if target, ok := privateIndexedFunctionGlobal(m.Globals[0], m); !ok || target != 0 {
		t.Fatal("valid private indexed function global", target, ok)
	}
	remaining := MaxModuleBytes
	if err := admitClosedReferenceNumericState(m, false, &remaining); err != nil {
		t.Fatal("valid typed cell state", err)
	}
	state, err := admitStatePolicy(m, true, false, true, false)
	if err != nil || len(state.globals) != 1 || !state.globals[0].wide || !state.globals[0].reference || state.globals[0].mutable {
		t.Fatal("typed cell carrier state", state.globals, err)
	}

	for name, mutate := range map[string]func(*wasm.Module){
		"mutable": func(v *wasm.Module) { v.Globals[0].Type.Mutable = true },
		"nullable": func(v *wasm.Module) {
			v.Globals[0].Type.Type = wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
		},
		"exact": func(v *wasm.Module) {
			v.Globals[0].Type.Type = wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), true))
		},
		"abstract": func(v *wasm.Module) {
			v.Globals[0].Type.Type = wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false))
		},
		"recursive": func(v *wasm.Module) {
			v.Globals[0].Type.Type = wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Rec: true}), false))
		},
		"wrong-target":   func(v *wasm.Module) { v.Globals[0].Init.BodyBytes = []byte{0xd2, 1, 0x0b} },
		"missing-target": func(v *wasm.Module) { v.Globals[0].Init.BodyBytes = []byte{0xd2, 2, 0x0b} },
		"null":           func(v *wasm.Module) { v.Globals[0].Init.BodyBytes = []byte{0xd0, 0, 0x0b} },
		"structured":     func(v *wasm.Module) { v.Globals[0].Init.Instrs = []wasm.Instruction{{Kind: wasm.InstrRefFunc}} },
		"table":          func(v *wasm.Module) { v.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref()}}} },
		"memory":         func(v *wasm.Module) { v.Memories = []wasm.MemType{{}} },
		"data":           func(v *wasm.Module) { v.Data = []wasm.Data{{}} },
		"start":          func(v *wasm.Module) { index := wasm.FuncIdx(0); v.Start = &index },
		"import": func(v *wasm.Module) {
			v.Imports = []wasm.Import{{Type: wasm.NewFuncExternType(wasm.TypeIdx{})}}
		},
		"nonfunction": func(v *wasm.Module) { v.Types[0].SubTypes[0].Comp.Kind = wasm.CompStruct },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := privateIndexedFunctionGlobalFixture()
			mutate(candidate)
			if _, ok := privateIndexedFunctionGlobal(candidate.Globals[0], candidate); ok {
				t.Fatal("nearby typed cell admitted")
			}
		})
	}
}
