//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestStageFlushSpills(t *testing.T) {
	f := fn{a: &encoder.Asm{B: make([]byte, 0, 128)}, s: newStack(), spillFloor: 4, maxSpill: 11}
	vector := f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 3})
	high := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 10})
	left := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
	f.setStackGCRoot(left, true)
	right := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 1})
	f.pushBinOp(opAdd, mtI64)
	deferred := f.s.back()
	live := f.pushReg(RAX, mtI64)
	f.pinned = maskOf(RAX)
	values := [...]*elem{vector, high, left, right, deferred, live}
	original := [...]storage{vector.st, high.st, left.st, right.st, deferred.st, live.st}
	below := [...]*elem{vector, high, deferred}
	wantSlots := [...]uint32{12, 10, 14, 15}

	allocs := testing.AllocsPerRun(100, func() {
		for i, e := range values {
			e.st = original[i]
		}
		f.a.B = f.a.B[:0]
		f.maxSpill = 11
		f.stageFlushSpills(4, below[:])
	})
	if allocs != 0 {
		t.Fatalf("staging allocations = %v, want zero", allocs)
	}
	for i, e := range values {
		want := original[i]
		if i < len(wantSlots) {
			want.slot = wantSlots[i]
		}
		if e.st != want {
			t.Fatalf("value %d storage = %+v, want %+v", i, e.st, want)
		}
	}
	if f.spillFloor != 4 || f.maxSpill != 16 || f.regUser[RAX] != live || f.pinned != maskOf(RAX) {
		t.Fatal("staging changed the floor or ownership, or reserved the wrong frame")
	}
	var want encoder.Asm
	want.Store64(RSP, f.spillOff(11), RAX)
	for _, move := range [][2]int{{3, 12}, {4, 13}, {0, 14}, {1, 15}} {
		want.Load64(RAX, RSP, f.spillOff(move[0]))
		want.Store64(RSP, f.spillOff(move[1]), RAX)
	}
	want.Load64(RAX, RSP, f.spillOff(11))
	if !bytes.Equal(f.a.B, want.B) {
		t.Fatalf("staging code = %x, want %x (including live RAX preservation)", f.a.B, want.B)
	}
}

func TestStageFlushCanonicalSlots(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack(), spillFloor: 3, maxSpill: 3}
	scalar := f.pushValue(storage{kind: stSlot, typ: mtF64, slot: 0})
	vector := f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 1})
	arg := f.pushValue(storage{kind: stConst, typ: mtI64, cval: 1})
	f.stageFlushSpills(3, []*elem{scalar, vector})
	if len(f.a.B) != 0 || f.maxSpill != 3 || scalar.st.slot != 0 || vector.st.slot != 1 {
		t.Fatal("canonical roots were staged")
	}
	arg.st = storage{kind: stSlot, typ: mtI64, slot: 0}
	f.stageFlushSpills(3, []*elem{scalar, vector})
	if arg.st.slot != 4 || f.maxSpill != 5 || scalar.st.slot != 0 || vector.st.slot != 1 {
		t.Fatal("overlapping argument was not staged independently")
	}
}
