package frontend

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestMaxLocalFuncSlotsCountsV128AsTwo(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{
			{SubTypes: []wasm.SubType{
				{Final: true, Comp: wasm.CompType{
					Kind:    wasm.CompFunc,
					Params:  []wasm.ValType{wasm.I32, wasm.V128},
					Results: []wasm.ValType{wasm.V128, wasm.I64},
				}},
			}},
		},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
	}
	params, results := (supportPass{m: m}).maxLocalFuncSlots()
	if params != 3 || results != 3 {
		t.Fatalf("wrapper slots params/results = %d/%d, want 3/3", params, results)
	}
}
