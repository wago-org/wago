package wago

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestValidatedTypedReferenceRequirementParity(t *testing.T) {
	for _, op := range []byte{0xd4, 0xd5, 0xd6} {
		t.Run(fmt.Sprintf("op_%x", op), func(t *testing.T) {
			body := []byte{0x20, 0, op}
			if op != 0xd4 {
				body = append(body, 0)
			}
			var results []wasm.ValType
			if op == 0xd6 {
				results = []wasm.ValType{wasm.FuncRef}
				body = append(body, 0x00)
			} else {
				body = append(body, 0x1a)
			}
			body = append(body, 0x0b)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, results))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			var analysis wasm.ValidatedModuleAnalysis
			if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
				t.Fatal(err)
			}
			want, got := analyzeModuleRequirements(m), analyzeModuleRequirementsWithValidation(m, &analysis)
			if !reflect.DeepEqual(got, want) || !got.features.IsEnabled(CoreFeatureTypedFunctionReferences) {
				t.Fatalf("summary %#v; exact %#v: typed references required", got, want)
			}
		})
	}
}

func TestValidatedTypeIndexedControlRequirementsAndArtifact(t *testing.T) {
	for _, op := range []byte{0x02, 0x03, 0x04} {
		for _, results := range []int{0, 1} {
			t.Run(fmt.Sprintf("op_%x/results_%d", op, results), func(t *testing.T) {
				var resultTypes []wasm.ValType
				if results == 1 {
					resultTypes = []wasm.ValType{wasm.I32}
				}
				var body []byte
				if op == 0x04 {
					body = append(body, 0x41, 1)
				}
				body = append(body, op, 1)
				if results == 1 {
					body = append(body, 0x41, 7)
				}
				if op == 0x04 && results == 1 {
					body = append(body, 0x05, 0x41, 8)
				}
				body = append(body, 0x0b)
				if results == 1 {
					body = append(body, 0x1a)
				}
				body = append(body, 0x0b)
				data := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, resultTypes))),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
				)
				m, err := wasm.DecodeModule(data)
				if err != nil {
					t.Fatal(err)
				}
				var analysis wasm.ValidatedModuleAnalysis
				if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
					t.Fatal(err)
				}
				want, got := analyzeModuleRequirements(m), analyzeModuleRequirementsWithValidation(m, &analysis)
				if !reflect.DeepEqual(got, want) || got.features != CoreFeatureMultiValue {
					t.Fatalf("summary %#v; exact %#v; want multi-value", got, want)
				}
				compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), data)
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				blob, err := compiled.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := LoadTrustedArtifact(blob)
				if err != nil {
					t.Fatal(err)
				}
				defer loaded.Close()
				if compiled.requiredFeatures != CoreFeatureMultiValue || loaded.requiredFeatures != CoreFeatureMultiValue {
					t.Fatalf("compiled %s, loaded %s: want multi-value", compiled.requiredFeatures, loaded.requiredFeatures)
				}
			})
		}
	}
}
