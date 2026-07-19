package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestModuleRequiredFeaturesFindsSIMDInEveryExpressionForm(t *testing.T) {
	simdBytes := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	simdBytes = append(simdBytes, 0x0b)
	simdExpr := wasm.Expr{BodyBytes: simdBytes}

	cases := []struct {
		name string
		m    *wasm.Module
	}{
		{name: "global initializer", m: &wasm.Module{Globals: []wasm.Global{{Init: simdExpr}}}},
		{name: "table initializer", m: &wasm.Module{Tables: []wasm.Table{{Init: &simdExpr}}}},
		{name: "element offset", m: &wasm.Module{Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemActive, Offset: simdExpr}}}}},
		{name: "element expression", m: &wasm.Module{Elements: []wasm.Elem{{Kind: wasm.ElemKind{Kind: wasm.ElemFuncExprs, Exprs: []wasm.Expr{simdExpr}}}}}},
		{name: "data offset", m: &wasm.Module{Data: []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataActive, Offset: simdExpr}}}}},
		{name: "function body", m: &wasm.Module{Code: []wasm.Func{{BodyBytes: simdBytes}}}},
		{name: "programmatic function body", m: &wasm.Module{Code: []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrV128Const}}}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := moduleRequiredFeatures(tc.m); !got.IsEnabled(CoreFeatureSIMD) {
				t.Fatalf("moduleRequiredFeatures = %s, want SIMD", got)
			}
		})
	}
}

func TestRequiredFeaturesBodyScannerIgnoresSIMDByteInImmediate(t *testing.T) {
	features := requiredFeaturesForBodyBytes([]byte{0x41, 0xfd, 0x00, 0x0b})
	if features.IsEnabled(CoreFeatureSIMD) {
		t.Fatalf("requiredFeaturesForBodyBytes = %s for scalar i32.const", features)
	}
}
