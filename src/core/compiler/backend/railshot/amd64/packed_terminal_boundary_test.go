//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func packedBoundaryFixture(t *testing.T) (*fn, *elem) {
	t.Helper()
	f := packedTestFn()
	f.localType = []machineType{mtI32, mtI32}
	f.localSlot = []uint32{0, 8}
	f.locals = []localDef{{typ: mtI32, reg: R12, state: lsReg}, {typ: mtI32, reg: R8, state: lsStackReg}}
	f.pinnedLocals = []int{0, 1}
	f.pinnedLocalMask = maskOf(R12).union(maskOf(R8))
	l := f.s.pushValue(storage{kind: stLocalReg, typ: mtI32, idx: 0, reg: R12})
	r := f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
	if !f.tryPackTerminal(opAdd, mtI32, l, r) {
		t.Fatal("not admitted")
	}
	return f, l
}

func TestPackedTerminalStableLocalBoundary(t *testing.T) {
	packedTestEnabled(t)
	for _, index := range []uint32{0, 1} {
		f, n := packedBoundaryFixture(t)
		original := *n
		depth, effects := f.s.logicalDepth, f.s.pendingEffects
		f.packedOpcodeBoundary(0x20)
		f.packedLocalGetBoundary(index)
		if *n != original || f.s.packedCount != 1 || f.s.logicalDepth != depth || f.s.pendingEffects != effects || len(f.a.B) != 0 {
			t.Fatal("safe read changed retained expression")
		}
		// Retention must not hide the older local read from a subsequent overwrite.
		f.realizeLocalRefs(0, nil)
		if n.isPackedTerminal() || f.s.packedCount != 0 || n.arg0 == nil || n.arg0.st.kind != stReg {
			t.Fatal("retained old local was not captured")
		}
	}
}

func TestPackedTerminalUnsafeLocalBoundary(t *testing.T) {
	packedTestEnabled(t)
	cases := []struct {
		name  string
		edit  func(*fn)
		index uint32
	}{
		{"inline", func(f *fn) { f.inlineDepth = 1 }, 0},
		{"forward", func(f *fn) { f.inlineForwardN = 1 }, 0},
		{"relinquished", func(f *fn) { f.pinRelinquished = true }, 0},
		{"float", func(f *fn) { f.localType[0] = mtF32 }, 0},
		{"zero-state", func(f *fn) { f.locals[0].state = lsConstZero }, 0},
		{"gc-local", func(f *fn) { f.gcLocalTypes = []uint64{1} }, 0},
		{"out-of-range", func(f *fn) {}, 99},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, n := packedBoundaryFixture(t)
			c.edit(f)
			f.packedLocalGetBoundary(c.index)
			if n.isPackedTerminal() || f.s.packedCount != 0 || n.arg0 == nil || n.arg1 == nil || len(f.a.B) != 0 {
				t.Fatal("unsafe boundary did not expand without emission")
			}
		})
	}
}

func TestPackedTerminalSuccessorAdmission(t *testing.T) {
	packedTestEnabled(t)
	cases := []struct {
		next  []byte
		allow bool
	}{
		{[]byte{0x20, 0}, true}, {[]byte{0x21, 0}, true}, {[]byte{0x1a}, true}, {[]byte{0x41, 3}, true},
		{[]byte{0x04, 0x40}, false}, {[]byte{0x28, 2, 0}, false}, {[]byte{0x0b}, false}, {nil, false},
	}
	for _, c := range cases {
		f := packedTestFn()
		f.s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
		f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
		r := wasm.NewReader(c.next)
		before := r.Offset()
		f.pushBinOpNext(r, opAdd, mtI32)
		if r.Offset() != before {
			t.Fatal("successor lookahead consumed source")
		}
		if f.s.back().isPackedTerminal() != c.allow {
			t.Fatalf("next %x admission differs", c.next)
		}
	}
	f := packedTestFn()
	f.s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
	f.pushBinOp(opAdd, mtI32)
	if f.s.back().isPackedTerminal() || f.s.packedCount != 0 {
		t.Fatal("unknown internal successor packed")
	}
}
