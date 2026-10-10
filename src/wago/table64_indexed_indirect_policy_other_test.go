//go:build !riscv64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestIndexedTwoTable64IndirectPolicyRemainsRV64Only(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}}}},
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I32}}}}},
		},
		FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 1}, {Index: 1}},
		Tables: []wasm.Table{
			{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Addr64: true, Min: 2, Max: 2, HasMax: true}}},
			{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Addr64: true, Min: 3, Max: 3, HasMax: true}}},
		},
		Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}},
		Code: []wasm.Func{
			{BodyBytes: []byte{0x41, 7, 0x0b}},
			{BodyBytes: []byte{0x20, 0, 0xd2, 0, 0x26, 0, 0x20, 0, 0x11, 0, 0, 0x0b}},
			{BodyBytes: []byte{0x20, 0, 0xd2, 0, 0x26, 1, 0x20, 0, 0x11, 0, 1, 0x0b}},
		},
		Exports: []wasm.Export{
			{Name: "call0", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}},
			{Name: "call1", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}},
			{Name: "table0", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}},
			{Name: "table1", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 1}},
		},
	}
	if !stagedOwnedTwoTable64IndirectShape(m) {
		t.Fatal("policy fixture no longer matches the exact indexed RV64 source shape")
	}
	if err := stagedTwoLocalTableShape(m); err == nil {
		t.Fatal("indexed RV64 call_indirect shape widened non-RV64 two-table admission")
	}
	m.Types[1].SubTypes[0].Comp.Params[0] = wasm.I32
	for i := range m.Tables {
		m.Tables[i].Type.Limits.Addr64 = false
	}
	if !stagedOwnedTwoTable32IndirectShape(m) {
		t.Fatal("policy fixture no longer matches the exact indexed table32 RV64 source shape")
	}
	if err := stagedTwoLocalTableShape(m); err == nil {
		t.Fatal("indexed table32 RV64 call_indirect shape widened non-RV64 two-table admission")
	}
}
