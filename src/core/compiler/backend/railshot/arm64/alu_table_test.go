//go:build arm64

package arm64

import (
	"testing"
	"unsafe"
)

func TestALUTableStaysCompactAndComplete(t *testing.T) {
	want := map[wOp]aluEnc{
		opAdd: {op: opAdd, comm: true},
		opSub: {op: opSub},
		opAnd: {op: opAnd, comm: true},
		opOr:  {op: opOr, comm: true},
		opXor: {op: opXor, comm: true},
	}
	if got := unsafe.Sizeof(aluTable); got != 12 {
		t.Fatalf("ALU encoding table = %d bytes, want 12", got)
	}
	for op, enc := range want {
		if got := aluTable[op]; got != enc {
			t.Errorf("ALU encoding %d = %+v, want %+v", op, got, enc)
		}
	}
}
