//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"
)

// A table with no declared maximum used a fixed 1,024-entry capacity, so a
// table that started at or above it could never grow.
func TestNoMaximumTableGrowsPastFixedReserve(t *testing.T) {
	if got := invokeI32(t, admissionModule(t, noMaxTableGrowWasm), "f"); got != 2000 {
		t.Fatalf("table.grow = %d, want old size 2000", got)
	}
}

// noMaxTableGrowWasm encodes:
//
//	(module (table 2000 funcref)
//	  (func (export "f") (result i32) (table.grow 0 (ref.null func) (i32.const 100))))
const noMaxTableGrowWasm = "0061736d010000000105016000017f030201000405017000d00f070501016600000a0c010a00d07041e400fc0f000b"
