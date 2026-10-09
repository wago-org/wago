package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReferenceImportsValidationAnalysisParity(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-tails%t", vector, tails), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImports(vector, tails, true))
				if err != nil {
					t.Fatal(err)
				}
				var analysis wasm.ValidatedModuleAnalysis
				if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{MultiMemory: true, ExtendedConstGlobals: true}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
					t.Fatal(err)
				}
				if !analysis.ValidFor(m) || moduleDynamicFuncrefEscape(m) || moduleDynamicFuncrefEscapeWithValidation(m, &analysis) || moduleDynamicFuncrefEscapeWithValidation(m, nil) {
					t.Fatal("ordinary imports with live private references imply dynamic escape")
				}
			})
		}
	}
	// A valid indirect call carrying a reference must still report escape.
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))),
		wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 1}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x11, 0, 0, 0x0b}))))
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	var analysis wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
		t.Fatal(err)
	}
	if !moduleDynamicFuncrefEscape(m) || !moduleDynamicFuncrefEscapeWithValidation(m, &analysis) || !moduleDynamicFuncrefEscapeWithValidation(m, nil) {
		t.Fatal("dynamic reference-valued call lost escape")
	}
}
