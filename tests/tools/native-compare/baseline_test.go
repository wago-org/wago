package main

import "testing"

// The intentionally weak baseline projects only operation names, like the
// opcode-only comparison discussed by issue 815. It is not a production decoder.
func TestOpcodeOnlyBaselineBlindSpots(t *testing.T) {
	for _, name := range []string{"constant", "zero-idiom-dependency", "spill-width", "stack-displacement"} {
		a, b := []string{"mov", "xor", "mov"}, []string{"mov", "xor", "mov"}
		if len(a) != len(b) { t.Fatal(name) }
		for i := range a { if a[i] != b[i] { t.Fatal(name) } }
		t.Logf("%s: opcode-only projection cannot distinguish operands", name)
	}
}
