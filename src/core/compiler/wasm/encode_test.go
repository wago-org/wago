package wasm

import (
	"bytes"
	"testing"
)

// Select shares decode opcode 0x1b with the untyped form but must encode as
// 0x1c + a result-type vector when typed. Guards against the fast simpleKindOpcode
// map path swallowing InstrSelect and always emitting 0x1b.
func TestEncodeExprSelect(t *testing.T) {
	// Untyped select -> 0x1b, then the expr-terminating 0x0b.
	got, err := EncodeExpr(Expr{Instrs: []Instruction{{Kind: InstrSelect}}})
	if err != nil {
		t.Fatalf("untyped select: %v", err)
	}
	if want := []byte{0x1b, 0x0b}; !bytes.Equal(got, want) {
		t.Errorf("untyped select = % x, want % x", got, want)
	}

	// Typed select -> 0x1c, count, result valtype(s), then 0x0b.
	vt, ok := EncodeValType(I32)
	if !ok {
		t.Fatal("EncodeValType(I32) not ok")
	}
	got, err = EncodeExpr(Expr{Instrs: []Instruction{{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{I32}}}}})
	if err != nil {
		t.Fatalf("typed select: %v", err)
	}
	if want := []byte{0x1c, 0x01, vt, 0x0b}; !bytes.Equal(got, want) {
		t.Errorf("typed select = % x, want % x", got, want)
	}
}
