package frontend

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestDirectTailGateRoutesReturnCallFamiliesSeparately(t *testing.T) {
	for _, tc := range []struct {
		name string
		body wasm.Func
	}{
		{name: "decoded AST", body: wasm.Func{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrReturnCall, Index: 1}}}}},
		{name: "byte backed", body: wasm.Func{BodyBytes: []byte{0x12, 0x01, 0x0b}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &wasm.Module{
				Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}}}}},
				FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 0}},
				Code:      []wasm.Func{tc.body, {Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}}}}},
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
			feat := AllFeatures()
			if err := RejectUnsupportedWithFeatures(m, feat); err == nil || !strings.Contains(err.Error(), "tail-call disabled") {
				t.Fatalf("default gate error = %v", err)
			}
			feat.TailCalls = true
			if err := RejectUnsupportedWithFeatures(m, feat); err != nil {
				t.Fatalf("staged direct-tail support: %v", err)
			}
		})
	}
}

func TestTypedTailGateRoutesReturnCallRefSeparately(t *testing.T) {
	indexed := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
	m := &wasm.Module{
		Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{
			Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, indexed}, Results: []wasm.ValType{wasm.I32},
		}}}}, {SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{
			Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32},
		}}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 1}},
		Code: []wasm.Func{
			{BodyBytes: []byte{0x20, 0x00, 0x20, 0x01, 0x15, 0x01, 0x0b}},
			{BodyBytes: []byte{0x20, 0x00, 0x0b}},
		},
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
	feat := AllFeatures()
	feat.TypedFunctionReferences = true
	if err := RejectUnsupportedWithFeatures(m, feat); err == nil || !strings.Contains(err.Error(), "typed reference tail calls disabled") {
		t.Fatalf("typed-only gate error = %v", err)
	}
	feat.TypedTailCalls = true
	if err := RejectUnsupportedWithFeatures(m, feat); err != nil {
		t.Fatalf("staged typed-tail support: %v", err)
	}
}
