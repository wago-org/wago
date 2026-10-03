package wago

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCPinnedProductsRejectUnmatchedSizesWithoutAllocating(t *testing.T) {
	for _, size := range []int{0, 8, 64, 4096, 1 << 20} {
		data := make([]byte, size)
		if allocations := testing.AllocsPerRun(10, func() {
			if _, ok := stagedGCStructExecutionProduct(data); ok {
				t.Fatal("unexpected struct fixture match")
			}
			if _, ok := stagedGCArrayExecutionProduct(data); ok {
				t.Fatal("unexpected array fixture match")
			}
			if _, ok := stagedGCI31ExecutionProduct(data); ok {
				t.Fatal("unexpected i31 fixture match")
			}
		}); allocations != 0 {
			t.Fatalf("size %d: unmatched fixture checks allocated %.0f times", size, allocations)
		}
	}
}

func TestGCCompileScanRequirementGate(t *testing.T) {
	scalar := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x1a, 0x0b}))),
	)
	conversion := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 0x1a, 0x1a, 0x0b}))),
	)
	for _, tc := range []struct {
		name                string
		data                []byte
		gc, generic, extern bool
	}{
		{"scalar", scalar, false, false, false},
		{"struct", stagedGCStructGetOnlyBytes(t), true, true, false},
		{"extern_conversion", conversion, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			var analysis wasm.ValidatedModuleAnalysis
			if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{GCConstExpr: true}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
				t.Fatal(err)
			}
			if !analysis.ValidFor(m) {
				t.Fatal("expected byte-backed validation facts")
			}
			required := analyzeModuleRequirementsWithValidation(m, &analysis).features.IsEnabled(CoreFeatureGC)
			if required != tc.gc {
				t.Fatalf("GC requirement = %t, want %t", required, tc.gc)
			}
			if got := required && moduleUsesGenericGCStructHelpers(m); got != tc.generic {
				t.Fatalf("generic helpers = %t, want %t", got, tc.generic)
			}
			if got := required && moduleUsesGCExternConversion(m); got != tc.extern {
				t.Fatalf("extern conversion = %t, want %t", got, tc.extern)
			}
		})
	}
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	features := cfg.frontendFeatures()
	features.GCStructProducts = true
	withGC, err := compileWithFrontendFeatures(cfg, scalar, features)
	if err != nil {
		t.Fatal(err)
	}
	defer withGC.Close()
	features.GCStructProducts = false
	withoutGC, err := compileWithFrontendFeatures(cfg, scalar, features)
	if err != nil {
		t.Fatal(err)
	}
	defer withoutGC.Close()
	var got, want bytes.Buffer
	if _, err := withGC.WriteCodeTo(&got); err != nil {
		t.Fatal(err)
	}
	if _, err := withoutGC.WriteCodeTo(&want); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatal("GC feature support changed non-GC native code")
	}
}

func TestGCCompileScanGateIncludesInitializerConversions(t *testing.T) {
	expr := wasm.Expr{BodyBytes: []byte{0xd0, 0x6f, 0xfb, 0x1a, 0x0b}}
	for _, tc := range []struct {
		name   string
		module *wasm.Module
	}{
		{"global", &wasm.Module{Globals: []wasm.Global{{Init: expr}}}},
		{"table", &wasm.Module{Tables: []wasm.Table{{Init: &expr}}}},
		{"element", &wasm.Module{Elements: []wasm.Elem{{Kind: wasm.ElemKind{Exprs: []wasm.Expr{expr}}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !moduleUsesGCExternConversion(tc.module) {
				t.Fatal("expected conversion in initializer")
			}
			if !analyzeModuleRequirements(tc.module).features.IsEnabled(CoreFeatureGC) {
				t.Fatal("initializer conversion must pass GC scan gate")
			}
		})
	}
}
