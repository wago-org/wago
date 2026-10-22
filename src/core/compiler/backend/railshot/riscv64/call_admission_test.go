//go:build linux && !tinygo

package riscv64

import "testing"

// This exact portable fixture is also run against the accepted pre-call tree
// to record the previously unsupported opcode before its execution tests.
func TestDirectCallScalarLeafAdmission(t *testing.T) {
	b := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x10, 1, 0x0b}}, fixtureFunc{result: 0x7f, body: []byte{0x41, 3, 0x0b}})
	if c, err := Compile(b); err != nil || c == nil {
		t.Fatal("direct scalar leaf admission", err)
	}
}
