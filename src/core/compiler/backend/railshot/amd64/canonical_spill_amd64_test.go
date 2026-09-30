//go:build amd64

package amd64

import (
	"testing"

	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestStageMixedCallSpillsAMD64(t *testing.T) {
	f := fn{a: &x64.Asm{B: make([]byte, 0, 256)}, s: newStack(), spillFloor: 4, maxSpill: 11}
	vector := f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 3})
	high := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 10})
	left := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
	left.st.setGCRoot(true)
	right := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 1})
	f.pushBinOp(opAdd, mtI64)
	deferred := f.s.back()
	if !deferred.isDeferred() {
		t.Fatal("expected a deferred expression")
	}
	above := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 2})
	live := f.pushReg(RAX, mtI64)
	f.pinned = maskOf(RAX)
	values := [...]*elem{vector, high, left, right, deferred, above, live}
	original := [...]storage{vector.st, high.st, left.st, right.st, deferred.st, above.st, live.st}
	below := [...]*elem{vector, high, deferred}
	wantSlots := [...]uint32{12, 10, 14, 15}

	allocs := testing.AllocsPerRun(100, func() {
		for i, e := range values {
			e.st = original[i]
		}
		f.a.B = f.a.B[:0]
		f.maxSpill = 11
		f.stageCanonicalSpills(4, below[:])
	})
	if allocs != 0 {
		t.Fatalf("relocation allocations = %v, want zero", allocs)
	}
	for i, e := range values {
		want := original[i]
		if i < len(wantSlots) {
			want.slot = wantSlots[i]
		}
		if e == above {
			want.slot = 16
		}
		if e.st != want {
			t.Fatalf("value %d storage = %+v, want %+v", i, e.st, want)
		}
	}
	if f.spillFloor != 4 || f.maxSpill != 17 {
		t.Fatalf("floor/max = %d/%d, want 4/17", f.spillFloor, f.maxSpill)
	}
	if f.regUser[RAX] != live || f.pinned != maskOf(RAX) {
		t.Fatal("relocation changed register ownership")
	}
	if len(f.a.B) == 0 {
		t.Fatal("conflicting sources were not copied")
	}
}
