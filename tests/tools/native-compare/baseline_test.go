//go:build wago_nativecompare

package main

import (
	"encoding/hex"
	"testing"
)

var controls = []struct{ name, a, b string }{
	{"constant", "b82a000000", "b82b000000"},
	{"zero-idiom-dependency", "31c0", "31c8"},
	{"spill-width", "89442408", "4889442408"},
	{"stack-displacement", "89442408", "89442410"},
	{"relocation", "e901000000", "e902000000"},
}

func unhex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// Opcode-only projection for these controlled instructions ignores operands,
// including REX width. It is deliberately not a general decoder.
func opcodeOnly(b []byte) byte {
	if b[0]&0xf0 == 0x40 {
		return b[1]
	}
	return b[0]
}
func TestOpcodeOnlyBaselineBlindSpots(t *testing.T) {
	for _, c := range controls {
		if c.a == c.b {
			t.Fatal("control must have distinct bytes")
		}
		if opcodeOnly(unhex(c.a)) != opcodeOnly(unhex(c.b)) {
			t.Fatal(c.name)
		}
		t.Logf("%s: %s -> %s: opcode-only equal", c.name, c.a, c.b)
	}
}
