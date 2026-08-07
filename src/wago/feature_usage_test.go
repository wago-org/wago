package wago

import (
	"bytes"
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

func TestRequiredFeaturesFindsThreadsDeclarationsAndInstructions(t *testing.T) {
	max := uint64(1)
	shared := wasm.MemType{Shared: true, Limits: wasm.Limits{Min: 1, Max: &max}}
	for _, tc := range []struct {
		name string
		m    *wasm.Module
	}{
		{name: "imported shared memory", m: &wasm.Module{Imports: []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternMem, Mem: shared}}}}},
		{name: "local shared memory", m: &wasm.Module{Memories: []wasm.MemType{shared}}},
		{name: "atomic instruction", m: &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{0xfe, 0x1e, 0x02, 0x00, 0x0b}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := moduleRequiredFeatures(tc.m); !got.IsEnabled(CoreFeatureThreads) {
				t.Fatalf("moduleRequiredFeatures = %s, want threads", got)
			}
		})
	}
}

func TestRequiredFeaturesBodyScannerFastImmediatePaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want CoreFeatures
	}{
		{name: "throw", body: []byte{0x08, 0x00, 0x0b}, want: CoreFeatureExceptionHandling},
		{name: "return_call", body: []byte{0x12, 0x00, 0x0b}, want: CoreFeatureTailCall},
		{name: "call_ref", body: []byte{0x14, 0x00, 0x0b}, want: CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences},
		{name: "return_call_ref", body: []byte{0x15, 0x00, 0x0b}, want: CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureTailCall},
		{name: "table_get", body: []byte{0x25, 0x00, 0x0b}, want: CoreFeatureReferenceTypes},
		{name: "ref_func", body: []byte{0xd2, 0x00, 0x0b}, want: CoreFeatureReferenceTypes},
		{name: "sign_extension", body: []byte{0xc0, 0x0b}, want: CoreFeatureSignExtensionOps},
		{name: "throw_ref", body: []byte{0x0a, 0x0b}, want: CoreFeatureExceptionHandling},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := requiredFeaturesForBodyBytes(tc.body); got != tc.want {
				t.Fatalf("features = %s, want %s", got, tc.want)
			}
		})
	}
}

func BenchmarkRequiredFeaturesScalarBody(b *testing.B) {
	// A representative scalar stream: two immediate-bearing constants followed by
	// two immediate-free ALU/stack instructions. The feature scanner must still
	// consume every immediate exactly rather than searching raw opcode bytes.
	body := bytes.Repeat([]byte{0x41, 1, 0x41, 2, 0x6a, 0x1a}, 16<<10)
	body = append(body, 0x0b)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := requiredFeaturesForBodyBytes(body); got != 0 {
			b.Fatalf("features = %s, want none", got)
		}
	}
}
