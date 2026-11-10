//go:build amd64

package amd64

import (
	"testing"
	"unsafe"
)

func TestALUTableStaysCompactAndComplete(t *testing.T) {
	want := map[wOp]aluEnc{
		opAdd: {rr: 0x01, rm: 0x03, digit: 0, comm: true},
		opSub: {rr: 0x29, rm: 0x2b, digit: 5},
		opAnd: {rr: 0x21, rm: 0x23, digit: 4, comm: true},
		opOr:  {rr: 0x09, rm: 0x0b, digit: 1, comm: true},
		opXor: {rr: 0x31, rm: 0x33, digit: 6, comm: true},
	}
	if got := unsafe.Sizeof(aluTable); got != 24 {
		t.Fatalf("ALU encoding table = %d bytes, want 24", got)
	}
	for op, enc := range want {
		if got := aluTable[op]; got != enc {
			t.Errorf("ALU encoding %d = %+v, want %+v", op, got, enc)
		}
	}
}

var aluTableBenchSink aluEnc

func BenchmarkALUTableLookup(b *testing.B) {
	ops := [...]wOp{opAdd, opSub, opAnd, opOr, opXor}
	var enc aluEnc
	b.ReportMetric(256, "lookups/op")
	for i := 0; i < b.N; i++ {
		for j := 0; j < 256; j++ {
			enc = aluTable[ops[j%len(ops)]]
		}
	}
	aluTableBenchSink = enc
}
