//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestDroppedLiteralPreservesStack(t *testing.T) {
	literals := []struct {
		name      string
		op        byte
		immediate []byte
	}{
		{"i32-min", 0x41, []byte{0x80, 0x80, 0x80, 0x80, 0x78}},
		{"i32-max", 0x41, []byte{0xff, 0xff, 0xff, 0xff, 0x07}},
		{"i64-min", 0x42, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x7f}},
		{"i64-max", 0x42, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00}},
		{"f32-nan", 0x43, []byte{0x01, 0x00, 0x80, 0x7f}},
		{"f64-nan", 0x44, []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x7f}},
	}
	for _, literal := range literals {
		t.Run(literal.name, func(t *testing.T) {
			for _, suffix := range [][]byte{{0x1a, 0x1a}, {0x01}, nil} {
				f := &fn{s: newStack()}
				st := storage{kind: stReg, typ: mtI64, reg: RAX}
				st.setGCRoot(true)
				prefix := f.s.pushValue(st)
				beforeStorage := prefix.st
				f.s.canonicalSlots = true
				beforeUsed, beforeReserved := f.s.nodeMemory()
				r := wasm.NewReader(append(append([]byte(nil), literal.immediate...), suffix...))
				if err := f.emitPlain(r, literal.op); err != nil {
					t.Fatal(err)
				}
				dropped := len(suffix) != 0 && suffix[0] == 0x1a
				if dropped {
					used, reserved := f.s.nodeMemory()
					if f.s.back() != prefix || prefix.st != beforeStorage || f.s.logicalDepth != 1 || !f.s.canonicalSlots || !f.s.hasGCRoots || used != beforeUsed || reserved != beforeReserved {
						t.Fatal("literal/drop changed the prefix, layout/root facts, or arena usage")
					}
					if r.Offset() != len(literal.immediate)+1 {
						t.Fatal("must consume exactly one adjacent drop")
					}
				} else if f.s.logicalDepth != 2 || f.s.back() == prefix || r.Offset() != len(literal.immediate) {
					t.Fatal("literal without adjacent drop must remain on the stack")
				}
			}
		})
	}
}

func TestDroppedLiteralRejectsTruncatedImmediate(t *testing.T) {
	for _, op := range []byte{0x41, 0x42, 0x43, 0x44} {
		f := &fn{s: newStack()}
		if err := f.emitPlain(wasm.NewReader([]byte{0x80}), op); err == nil {
			t.Fatalf("opcode %#x accepted a truncated immediate", op)
		}
	}
}
