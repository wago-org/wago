//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"
)

func TestGCNullConversionChains(t *testing.T) {
	if got := invokeI32(t, admissionModule(t, gcConversionChainWasm), "f"); got != 3 {
		t.Fatalf("null conversion chains = %d, want 3", got)
	}
}

// gcConversionChainWasm encodes:
//
//	(module
//	  (global $g1 externref (extern.convert_any (any.convert_extern (ref.null extern))))
//	  (global $g2 anyref (any.convert_extern (extern.convert_any (any.convert_extern (ref.null noextern)))))
//	  (table $t 4 externref (extern.convert_any (any.convert_extern (ref.null extern))))
//	  (func (export "f") (result i32)
//	    (i32.add (i32.add (ref.is_null (global.get $g1)) (ref.is_null (global.get $g2)))
//	             (ref.is_null (table.get $t (i32.const 3))))))
const gcConversionChainWasm = "0061736d010000000105016000017f03020100040d0140006f0004d06ffb1afb1b0b0615026f00d06ffb1afb1b0b6e00d072fb1afb1bfb1a0b070501016600000a11010f002300d12301d16a41032500d16a0b0016046e616d650504010001740709020002673101026732"
