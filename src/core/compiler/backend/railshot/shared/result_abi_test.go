package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestPlanScalarResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []wasm.ValType
		gp, fp  uint8
		ok      bool
	}{
		{name: "empty", ok: true},
		{name: "mixed four", results: []wasm.ValType{wasm.I32, wasm.F64, wasm.I64, wasm.F32}, gp: 2, fp: 2, ok: true},
		{name: "three gp", results: []wasm.ValType{wasm.I32, wasm.I64, wasm.I32}},
		{name: "three fp", results: []wasm.ValType{wasm.F32, wasm.F64, wasm.F32}},
		{name: "v128", results: []wasm.ValType{wasm.V128}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := PlanScalarResults(tc.results)
			if ok != tc.ok || got.GP != tc.gp || got.FP != tc.fp {
				t.Fatalf("plan = %+v, %t; want gp=%d fp=%d ok=%t", got, ok, tc.gp, tc.fp, tc.ok)
			}
		})
	}
}
