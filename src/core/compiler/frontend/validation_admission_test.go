package frontend

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestValidatedAdmissionPreservesExactFeatureChecks(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"ref_as_non_null", []byte{0x00, 0xd4, 0x1a, 0x0b}},
		{"br_on_null", []byte{0x00, 0xd5, 0, 0x1a, 0x0b}},
		{"br_on_non_null", []byte{0x00, 0xd6, 0, 0x0b}},
		{"ref_null_typed", []byte{0xd0, 0, 0x1a, 0x0b}},
		{"ref_null_gc", []byte{0xd0, byte(wasm.HeapAny), 0x1a, 0x0b}},
		{"ref_null_exception", []byte{0xd0, byte(wasm.HeapExn), 0x1a, 0x0b}},
		{"numeric", []byte{0x41, 1, 0x41, 2, 0x6a, 0x1a, 0x0b}},
		{"explicit_memory_zero", []byte{0x41, 0, 0x28, 0x42, 0, 0, 0x1a, 0x0b}},
		{"call_indirect_table64", []byte{0x42, 0, 0x11, 0, 0, 0x0b}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var results []wasm.ValType
			if tc.name == "br_on_non_null" {
				results = []wasm.ValType{wasm.FuncRef}
			}
			var tableSection []byte
			if tc.name == "call_indirect_table64" {
				tableSection = wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 4, 0}))
			}
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, results))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				tableSection,
				wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tc.body))),
			)
			m, err := wasm.DecodeModuleWithFeatures(data, wasm.ValidationFeatures{MultiMemory: true})
			if err != nil {
				t.Fatal(err)
			}
			var analysis wasm.ValidatedModuleAnalysis
			if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{MultiMemory: true}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
				t.Fatal(err)
			}
			var all Features
			value := reflect.ValueOf(&all).Elem()
			for i := 0; i < value.NumField(); i++ {
				if value.Field(i).Kind() == reflect.Bool {
					value.Field(i).SetBool(true)
				}
			}
			// Each switch is disabled on its own, including product-specific gates.
			for disabled := -1; disabled < value.NumField(); disabled++ {
				features := all
				if disabled >= 0 {
					field := reflect.ValueOf(&features).Elem().Field(disabled)
					if field.Kind() != reflect.Bool {
						continue
					}
					field.SetBool(false)
				}
				p := supportPass{m: m, feat: features, validation: &analysis, classifier: wasm.NewModuleInstructionClassifier(m, true), hasTable64: tc.name == "call_indirect_table64"}
				exact := p.funcExprBytes(tc.body, 0)
				fast := p.validatedFuncAccepted(0, &m.Code[0])
				if fast && exact != nil {
					t.Errorf("disabled field %d: fast admission bypassed %v", disabled, exact)
				}
				if tc.name == "numeric" && !fast {
					t.Errorf("numeric fast path lost with disabled field %d", disabled)
				}
			}
		})
	}
}
