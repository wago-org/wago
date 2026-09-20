//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestStageMixedCallSpillsARM64(t *testing.T) {
	f := fn{a: &a64.Asm{B: make([]byte, 0, 64)}, s: newStack(), spillFloor: 3, maxSpill: 11}
	vector := f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 2})
	high := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 10})
	left := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
	left.st.setGCRoot(true)
	right := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 1})
	f.pushBinOp(opAdd, mtI64)
	deferred := f.s.back()
	if !deferred.isDeferred() {
		t.Fatal("expected a deferred expression")
	}
	live := f.pushReg(X0, mtI64)
	f.pinned = maskOf(X0)
	values := [...]*elem{vector, high, left, right, deferred, live}
	original := [...]storage{vector.st, high.st, left.st, right.st, deferred.st, live.st}
	wantSlots := [...]uint32{11, 10, 13, 14}

	allocs := testing.AllocsPerRun(100, func() {
		for i, e := range values {
			e.st = original[i]
		}
		f.a.B = f.a.B[:0]
		f.maxSpill = 11
		f.stageMixedCallSpills(3)
	})
	if allocs != 0 {
		t.Fatalf("relocation allocations = %v, want zero", allocs)
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
	if f.spillFloor != 3 || f.maxSpill != 15 {
		t.Fatalf("floor/max = %d/%d, want 3/15", f.spillFloor, f.maxSpill)
	}
	if f.regUser[X0] != live || f.pinned != maskOf(X0) {
		t.Fatal("relocation changed register ownership")
	}
	if len(f.a.B) != 32 {
		t.Fatalf("copy code = %d bytes, want 32", len(f.a.B))
	}
	for off := 0; off < len(f.a.B); off += 4 {
		if rt := binary.LittleEndian.Uint32(f.a.B[off:]) & 31; rt != uint32(X16) {
			t.Fatalf("copy at %d uses register %d, want reserved X16", off, rt)
		}
	}
}
