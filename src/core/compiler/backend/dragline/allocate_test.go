package dragline

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAllocateLinearUsesFixedParamsAndSpills(t *testing.T) {
	fn := &railssa.Func{
		Params:  []wasm.ValType{wasm.I64, wasm.I64},
		Results: []wasm.ValType{wasm.I64},
		Values: []railssa.Value{
			{Type: wasm.I64, Op: railssa.OpParam},
			{Type: wasm.I64, Op: railssa.OpParam, Aux: 1},
			{Type: wasm.I64, Op: railssa.OpConst, Aux: 3},
			{Type: wasm.I64, Op: railssa.OpAdd, Args: railssa.Range{Start: 0, Len: 2}},
			{Type: wasm.I64, Op: railssa.OpXor, Args: railssa.Range{Start: 2, Len: 2}},
		},
		Args:   []railssa.ValueID{0, 1, 3, 2},
		Result: 4,
	}
	allocation, err := allocateLinear(fn, 2)
	if err != nil {
		t.Fatal(err)
	}
	if allocation.values[0] != (location{kind: locationRegister, index: 0}) || allocation.values[1] != (location{kind: locationRegister, index: 1}) {
		t.Fatalf("parameter locations = %#v", allocation.values[:2])
	}
	if allocation.frameBytes == 0 {
		t.Fatal("pressure did not create a spill frame")
	}
}
