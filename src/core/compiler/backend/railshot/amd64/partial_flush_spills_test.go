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
