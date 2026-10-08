//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestNativeTailDefinedFactoryCountsMatchFrozenOwnership(t *testing.T) {
	sig := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.F64}, Results: []wasm.ValType{wasm.V128}}
	f := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
	o := tailDynamicOwnerForTest(t, []tailRAFunction{f})
	f, _ = tailPublishedFunctionForTest(t, o.functions[0])
	owner, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, []int{0}, []wasm.CompType{sig})
	if err != nil {
		t.Fatal(err)
	}
	for _, sites := range []int{1, 4, 16} {
		metadata, code, err := NativeTailDefinedFactoryCountsBudget(sites, 3*sites, 1, 1, 1, 3, len(f.snapshot))
		if err != nil {
			t.Fatal(err)
		}
		actualMetadata, actualCode, err := NativeTailDefinedFactoryBudget(sites, 3*sites, 1, []*NativeTailDefinedOwner{owner}, len(f.snapshot))
		if err != nil || metadata != actualMetadata || code != actualCode {
			t.Fatal("count-only charge differs from frozen actual structures", metadata, actualMetadata, code, actualCode, err)
		}
	}
	if a := testing.AllocsPerRun(4, func() {
		if _, _, err := NativeTailDefinedFactoryCountsBudget(4, 12, 1, 1, 1, 3, len(f.snapshot)); err != nil {
			panic(err)
		}
	}); a != 0 {
		t.Fatal("count-only admission allocated", a)
	}
	for _, counts := range [][6]int{
		{0, 0, 1, 1, 1, 3}, {4097, 0, 1, 1, 1, 3}, {1, 513, 1, 1, 1, 3},
		{1, 3, 0, 1, 1, 3}, {1, 3, tailRAMaxRanges + 1, 1, 1, 3}, {1, 3, 1, tailRAMaxRanges + 1, tailRAMaxRanges + 1, 3},
		{1, 3, 1, 2, 1, 3}, {1, 3, 1, 1, 1025, 3}, {1, 3, 1, 1, 1, 513},
		{4096, 0, 128, 128, 1024, 0}, {4096, 4096 * 512, 1, 1, 1, 3},
	} {
		if _, _, err := NativeTailDefinedFactoryCountsBudget(counts[0], counts[1], counts[2], counts[3], counts[4], counts[5], len(f.snapshot)); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unbounded generation counts", counts, err)
		}
	}
}
