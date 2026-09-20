package wasm

import (
	"bytes"
	"testing"
)

func TestReviewMemoryIndexRoundTrip(t *testing.T) {
	want := []byte{0x28, 0x42, 0x01, 0x00, 0x0b}
	expr, err := decodeExpr(newReader(want), 0)
	if err != nil {
		t.Fatal(err)
	}
	if expr.Instrs[0].MemArg().Mem == nil || *expr.Instrs[0].MemArg().Mem != 1 {
		t.Fatal("decode did not select memory 1")
	}
	got, err := EncodeExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("encoded memory-1 load = % x, want % x", got, want)
	}
}
