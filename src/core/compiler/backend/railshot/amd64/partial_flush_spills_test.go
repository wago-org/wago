//go:build amd64

package amd64

import (
	"bytes"
	"fmt"
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestPartialFlushKeepsNewVectorSpillsAboveDestinations(t *testing.T) {
	for _, floor := range []int{0, 80} {
		t.Run(fmt.Sprint(floor), func(t *testing.T) {
			f := fn{a: &encoder.Asm{}, s: newStack(), spillFloor: floor, localSlot: []uint32{0}, nLocalSlots: 2}
			f.pushValue(storage{kind: stLocalRef, typ: mtV128})
			for reg := Reg(0); reg < 8; reg++ {
				f.fpinnedLocalMask = f.fpinnedLocalMask.add(reg)
			}
			for reg := Reg(8); reg < 16; reg++ {
				f.pushVReg(reg)
			}
			condition := f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
			f.flushBelow(condition)
			slots := 2 * (1 + 16 - 8)
			wantFn := fn{a: &encoder.Asm{}}
			wantFn.mov128StoreDisp(RSP, f.spillOff(max(floor, slots)), Reg(8))
			want := wantFn.a.B
			if !bytes.HasPrefix(f.a.B, want) {
				t.Fatalf("first spill = %x, want %x above canonical destinations", f.a.B[:len(want)], want)
			}
			if f.spillFloor != floor {
				t.Fatalf("spill floor=%d, want restored %d", f.spillFloor, floor)
			}
			if f.s.back() != condition || condition.st.kind != stConst || condition.st.cval != 1 {
				t.Fatal("partial flush changed condition")
			}
			roots := f.rootsBottomToTop()
			for i, root := range roots[:len(roots)-1] {
				if root.st.kind != stSlot || root.st.typ != mtV128 || root.st.slot != uint32(i*2) {
					t.Fatalf("root%d is not canonical: %+v", i, root.st)
				}
			}
		})
	}
}

// Sources above the boundary, including deferred children, are still live after
// the prefix has been stored. Their homes can overlap that prefix's destinations.
func TestPartialFlushPreservesLiveSuffix(t *testing.T) {
	for _, floor := range []int{0, 80} {
		t.Run(fmt.Sprint(floor), func(t *testing.T) {
			f := fn{a: &encoder.Asm{}, s: newStack(), spillFloor: floor, globalCellReg: regNone}
			prefix := f.pushValue(storage{kind: stConst, typ: mtI64, cval: 53})
			left := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
			right := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 1})
			f.pushBinOp(opAdd, mtI64)
			boundary := f.s.back()
			vector := f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 0})
			live := f.pushReg(RAX, mtI64)
			f.pinned = maskOf(RAX)
			f.setStackGCRoot(left, true)
			beforeBoundary, beforeRight, beforeLive := boundary.st, right.st, live.st
			if got := f.flushBelow(boundary); got != 1 {
				t.Fatalf("flushed %d roots, want 1", got)
			}
			if prefix.st.kind != stSlot || prefix.st.slot != 0 {
				t.Fatal("prefix was not canonicalized")
			}
			if left.st.slot < uint32(max(floor, 2)) || vector.st.slot <= left.st.slot {
				t.Fatalf("suffix sources not staged: scalar=%d vector=%d", left.st.slot, vector.st.slot)
			}
			if !left.st.hasGCRoot() || vector.st.typ != mtV128 || vector.st.kind != stSlot {
				t.Fatal("staging changed suffix metadata")
			}
			if boundary.st != beforeBoundary || right.st != beforeRight || live.st != beforeLive ||
				f.regUser[RAX] != live || f.pinned != maskOf(RAX) || f.spillFloor != floor {
				t.Fatal("partial flush changed the live suffix or caller's floor")
			}
			if roots := f.rootsBottomToTop(); len(roots) != 4 || roots[1] != boundary || roots[2] != vector || roots[3] != live {
				t.Fatal("partial flush changed the logical suffix")
			}
			if f.maxSpill < int(vector.st.slot)+2 {
				t.Fatal("frame does not cover the staged vector")
			}
		})
	}
}

func TestStageFlushCopiesToCanonicalSlotsBackwards(t *testing.T) {
	f := fn{a: &encoder.Asm{B: make([]byte, 0, 128)}, s: newStack(), spillFloor: 5, maxSpill: 10}
	prefix := f.pushValue(storage{kind: stConst, typ: mtI64, cval: 53})
	scalar := f.pushValue(storage{kind: stSlot, typ: mtF64, slot: 0})
	vector := f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 1})
	tail := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 3})
	tail.st.setGCRoot(true)
	high := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 9})
	live := f.pushReg(RAX, mtI64)
	f.pinned = maskOf(RAX)
	roots := [...]*elem{prefix, scalar, vector, tail}
	original := [...]storage{prefix.st, scalar.st, vector.st, tail.st}
	allocs := testing.AllocsPerRun(100, func() {
		for i, e := range roots {
			e.st = original[i]
		}
		f.a.B = f.a.B[:0]
		f.maxSpill = 10
		f.stageFlushSpills(5, roots[:])
	})
	if allocs != 0 {
		t.Fatalf("direct copy allocations = %v, want zero", allocs)
	}
	wantSlots := [...]uint32{0, 1, 2, 4}
	for i, root := range roots {
		want := original[i]
		want.slot = wantSlots[i]
		if root.st != want {
			t.Fatalf("root %d storage = %+v, want %+v", i, root.st, want)
		}
	}
	if high.st.slot != 9 || f.regUser[RAX] != live || f.pinned != maskOf(RAX) || f.spillFloor != 5 || f.maxSpill != 11 {
		t.Fatal("direct copy changed live suffix, register ownership, floor, or frame extent")
	}
	want := fn{a: &encoder.Asm{}}
	want.a.Store64(RSP, f.spillOff(10), RAX)
	// The vector shifts right one slot, so its high half must move first too.
	for _, move := range [][2]int{{3, 4}, {2, 3}, {1, 2}, {0, 1}} {
		want.a.Load64(RAX, RSP, f.spillOff(move[0]))
		want.a.Store64(RSP, f.spillOff(move[1]), RAX)
	}
	want.a.Load64(RAX, RSP, f.spillOff(10))
	if !bytes.Equal(f.a.B, want.a.B) {
		t.Fatalf("direct copy code = %x, want backward moves %x", f.a.B, want.a.B)
	}
}

func TestStageFlushKeepsPermutedSourcesDisjoint(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack(), spillFloor: 2}
	left := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 1})
	right := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	f.stageFlushSpills(2, []*elem{left, right})
	if left.st.slot != 3 || right.st.slot != 3+1 || f.maxSpill != 3+2 {
		t.Fatalf("permuted sources were not staged disjointly: %d %d max=%d", left.st.slot, right.st.slot, f.maxSpill)
	}
}

func TestStageFlushDirectSkipsSafeSlots(t *testing.T) {
	f := fn{a: &encoder.Asm{}, s: newStack(), spillFloor: 4, maxSpill: 6}
	canonical := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
	prefix := f.pushValue(storage{kind: stConst, typ: mtI64, cval: 53})
	moving := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 1})
	high := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 5})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	f.stageFlushSpills(4, []*elem{canonical, prefix, moving, high})
	if canonical.st.slot != 0 || moving.st.slot != 2 || high.st.slot != 5 || f.maxSpill != 7 {
		t.Fatal("direct copy changed a safe source or staged an unnecessary range")
	}
	want := fn{a: &encoder.Asm{}}
	want.a.Store64(RSP, f.spillOff(6), RAX)
	want.a.Load64(RAX, RSP, f.spillOff(1))
	want.a.Store64(RSP, f.spillOff(2), RAX)
	want.a.Load64(RAX, RSP, f.spillOff(6))
	if !bytes.Equal(f.a.B, want.a.B) {
		t.Fatalf("copy code = %x, want only the unsafe source copied: %x", f.a.B, want.a.B)
	}
}
