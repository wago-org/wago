//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestStageMixedCallSpillsARM64(t *testing.T) {
	f := fn{a: &a64.Asm{B: make([]byte, 0, 64)}, s: newStack(), spillFloor: 4, maxSpill: 11}
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
	live := f.pushReg(X0, mtI64)
	f.pinned = maskOf(X0)
	values := [...]*elem{vector, high, left, right, deferred, live}
	original := [...]storage{vector.st, high.st, left.st, right.st, deferred.st, live.st}
	below := [...]*elem{vector, high, deferred}
	wantSlots := [...]uint32{11, 10, 13, 14}

	allocs := testing.AllocsPerRun(100, func() {
		for i, e := range values {
			e.st = original[i]
		}
		f.a.B = f.a.B[:0]
		f.maxSpill = 11
		f.stageMixedCallSpills(4, below[:])
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
	if f.spillFloor != 4 || f.maxSpill != 15 {
		t.Fatalf("floor/max = %d/%d, want 4/15", f.spillFloor, f.maxSpill)
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

func TestStageMixedCallCanonicalSlotsARM64(t *testing.T) {
	for _, alias := range []bool{false, true} {
		f := fn{a: &a64.Asm{}, s: newStack(), spillFloor: 3, maxSpill: 3}
		scalar := f.pushValue(storage{kind: stSlot, typ: mtF64, slot: 0})
		vector := f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 1})
		arg := f.pushValue(storage{kind: stConst, typ: mtF64, cval: 1})
		if alias {
			arg.st = storage{kind: stSlot, typ: mtF64, slot: 0}
		}
		f.stageMixedCallSpills(3, []*elem{scalar, vector})
		if scalar.st.slot != 0 || vector.st.slot != 1 {
			t.Fatal("canonical roots moved")
		}
		wantBytes, wantMax := 0, 3
		if alias {
			wantBytes, wantMax = 8, 4
			if arg.st.slot != 3 {
				t.Fatal("overlapping argument was not relocated")
			}
		}
		if f.a.Len() != wantBytes || f.maxSpill != wantMax {
			t.Fatalf("alias=%t: bytes/max = %d/%d, want %d/%d", alias, f.a.Len(), f.maxSpill, wantBytes, wantMax)
		}
	}
}

func TestStageMixedCallConsumedDeferredSlotsARM64(t *testing.T) {
	f := fn{a: &a64.Asm{}, s: newStack(), spillFloor: 2, maxSpill: 2}
	left := f.pushValue(storage{kind: stConst, typ: mtI64, cval: 9})
	right := f.pushValue(storage{kind: stConst, typ: mtI64, cval: 10})
	f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
	f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 1})
	f.pushBinOp(opAdd, mtI64)
	arg := f.s.back()
	f.materialize(arg)
	before := f.a.Len()
	f.stageMixedCallSpills(2, []*elem{left, right})
	if f.a.Len() != before || f.maxSpill != 2 {
		t.Fatal("consumed deferred children were relocated")
	}
}
