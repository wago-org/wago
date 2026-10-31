//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionLocalCellsAggregateBudget(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			c := rv64NativeTailCompile(t, "reference-local-cells-budget", wasmtest.ClosedReferenceLocalCells(vector, true, true, 1), false)
			v := rv64ReferenceStartCopy(c)
			padding := make([]byte, railshot.MaxModuleBytes)
			v.PassiveData = append(slices.Clone(v.PassiveData), PassiveDataInit{})
			last := len(v.PassiveData) - 1
			lo, hi := 0, len(padding)
			for lo < hi {
				mid := (lo + hi + 1) / 2
				v.PassiveData[last].Bytes = padding[:mid]
				if rv64ValidateReferenceFunctions(v) == nil {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			v.PassiveData[last].Bytes = padding[:lo]
			if lo == 0 || rv64ValidateReferenceFunctions(v) != nil {
				t.Fatal("exact cell-bearing retained budget")
			}
			v.Globals = append(v.Globals, v.Globals[2])
			if err := rv64ValidateReferenceFunctions(v); err == nil {
				t.Fatal("extra reference cell escaped retained aggregate budget")
			}
			v.Globals = v.Globals[:len(v.Globals)-1]
			if err := rv64ValidateReferenceFunctions(v); err != nil {
				t.Fatal("retained cell budget not restored", err)
			}
			v.PassiveData[last].Bytes = padding[:lo+1]
			if err := rv64ValidateReferenceFunctions(v); err == nil {
				t.Fatal("retained cell budget+1 admitted")
			}
		})
	}
}
