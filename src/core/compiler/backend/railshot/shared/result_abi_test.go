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

func TestPlanScalarResultsExhaustive(t *testing.T) {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	for n := 0; n <= 5; n++ {
		results := make([]wasm.ValType, n)
		var visit func(int)
		visit = func(i int) {
			if i != n {
				for _, typ := range types {
					results[i] = typ
					visit(i + 1)
				}
				return
			}
			plan, ok := PlanScalarResults(results)
			gp, fp := 0, 0
			for _, typ := range results {
				if wasm.EqualValType(typ, wasm.F32) || wasm.EqualValType(typ, wasm.F64) {
					fp++
				} else {
					gp++
				}
			}
			wantOK := n <= 4 && gp <= 2 && fp <= 2
			if ok != wantOK {
				t.Fatalf("results %v accepted=%t, want %t (plan %+v)", results, ok, wantOK, plan)
			}
			if !wantOK {
				return
			}
			gp, fp = 0, 0
			for j, typ := range results {
				wantBank, wantIndex := ScalarResultGP, gp
				if wasm.EqualValType(typ, wasm.F32) || wasm.EqualValType(typ, wasm.F64) {
					wantBank, wantIndex = ScalarResultFP, fp
					fp++
				} else {
					gp++
				}
				if plan.Locations[j].Bank != wantBank || int(plan.Locations[j].Index) != wantIndex {
					t.Fatalf("results %v location %d = %+v, want bank=%d index=%d", results, j, plan.Locations[j], wantBank, wantIndex)
				}
			}
		}
		visit(0)
	}
	for _, typ := range []wasm.ValType{wasm.V128, wasm.FuncRef, wasm.ExternRef} {
		if plan, ok := PlanScalarResults([]wasm.ValType{typ}); ok {
			t.Fatalf("unsupported result %v accepted: %+v", typ, plan)
		}
	}
}
