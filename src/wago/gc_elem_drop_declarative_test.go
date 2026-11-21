//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import "testing"

// AMD64 routes elem.drop of segment zero through the GC array helper whenever
// the module uses array helpers (here for extern.convert_any). Without checked
// element roots the helper only zeroed the descriptor for three staged array
// products and otherwise failed with "gc array element segment 0 is
// unavailable", although dropping a declarative segment is a valid no-op.
func TestGCElemDropDeclarativeSegmentZero(t *testing.T) {
	if got := invokeI32(t, admissionModule(t, gcElemDropDeclarativeWasm), "f"); got != 11 {
		t.Fatalf("f = %d, want 11", got)
	}
}

// gcElemDropDeclarativeWasm encodes:
//
//	(module
//	  (type $s (struct (field i32)))
//	  (elem declare (ref null $s))
//	  (func (export "f") (result i32)
//	    (drop (extern.convert_any (struct.new_default $s)))
//	    (elem.drop 0)
//	    (elem.drop 0)
//	    (i32.const 11)))
const gcElemDropDeclarativeWasm = "0061736d010000000109025f017f006000017f0302010107050101660000090501076300000a12011000fb0100fb1b1afc0d00fc0d00410b0b"
