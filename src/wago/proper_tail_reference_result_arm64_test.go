//go:build (linux || darwin) && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func arm64WideProperTailReferenceModule(kind properTailBehaviorKind, targetBody []byte) []byte {
	params := make([]wasm.ValType, 9)
	for i := range params {
		params[i] = wasm.I32
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(params, []wasm.ValType{wasm.FuncRef}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
	}
	if kind == properTailIndirect {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))))
	if kind == properTailIndirect {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})))
	} else {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))))
	}
	caller := make([]byte, 0, 24)
	for range params {
		caller = append(caller, 0x41, 0x00)
	}
	switch kind {
	case properTailDirect:
		caller = append(caller, 0x12, 0x00)
	case properTailIndirect:
		caller = append(caller, 0x41, 0x00, 0x13, 0x00, 0x00)
	case properTailRef:
		caller = append(caller, 0xd2, 0x00, 0x15, 0x00)
	}
	caller = append(caller, 0x0b)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(targetBody), wasmtest.Code(caller))))
	return wasmtest.Module(sections...)
}

func TestARM64ProperTailReferenceResultContractsExecuteAcrossKinds(t *testing.T) {
	cases := []struct {
		name     string
		body     []byte
		null     bool
		function uint32
	}{
		{name: "non-null", body: []byte{0xd2, 0x00, 0x0b}, function: 0},
		{name: "null", body: []byte{0xd0, 0x70, 0x0b}, null: true},
	}
	for _, kind := range []properTailBehaviorKind{properTailDirect, properTailIndirect, properTailRef} {
		t.Run([]string{"direct", "indirect", "ref"}[kind], func(t *testing.T) {
			for _, objective := range []OptimizationObjective{OptimizeBalanced, OptimizeSize, OptimizeEmbedded} {
				t.Run(objective.String(), func(t *testing.T) {
					for _, tc := range cases {
						t.Run(tc.name, func(t *testing.T) {
							compiled := compileProperTailBehavior(t, properTailResultModule(kind, []wasm.ValType{wasm.FuncRef}, tc.body), objective, 2)
							in, err := instantiateCore(compiled, InstantiateOptions{})
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							got, err := in.Call(context.Background(), "run")
							if err != nil || len(got) != 1 || got[0].Type() != ValFuncRef || got[0].FuncRef().IsNull() != tc.null {
								t.Fatalf("proper-tail funcref result = %v, %v; null=%v", got, err, tc.null)
							}
							if !tc.null && !in.FuncRefMatchesFunction(got[0].FuncRef(), tc.function) {
								t.Fatalf("proper-tail funcref result lost function %d identity: %v", tc.function, got)
							}
						})
					}
				})
			}
		})
	}
}

func TestARM64ProperTailReferenceResultWrapperFallbacks(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		null bool
	}{
		{name: "non-null", body: []byte{0xd2, 0x00, 0x0b}},
		{name: "null", body: []byte{0xd0, 0x70, 0x0b}, null: true},
	}
	for _, kind := range []properTailBehaviorKind{properTailDirect, properTailIndirect, properTailRef} {
		t.Run([]string{"direct", "indirect", "ref"}[kind], func(t *testing.T) {
			for _, objective := range []OptimizationObjective{OptimizeBalanced, OptimizeSize, OptimizeEmbedded} {
				t.Run(objective.String(), func(t *testing.T) {
					for _, tc := range cases {
						t.Run(tc.name, func(t *testing.T) {
							compiled := compileProperTailBehavior(t, arm64WideProperTailReferenceModule(kind, tc.body), objective, 2)
							in, err := instantiateCore(compiled, InstantiateOptions{})
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							got, err := in.Call(context.Background(), "run")
							if err != nil || len(got) != 1 || got[0].Type() != ValFuncRef || got[0].FuncRef().IsNull() != tc.null {
								t.Fatalf("wide proper-tail funcref result = %v, %v; null=%v", got, err, tc.null)
							}
							if !tc.null && !in.FuncRefMatchesFunction(got[0].FuncRef(), 0) {
								t.Fatalf("wide proper-tail funcref result lost identity: %v", got)
							}
						})
					}
				})
			}
		})
	}
}
