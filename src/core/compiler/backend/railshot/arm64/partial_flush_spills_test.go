//go:build arm64

package arm64

import (
	"bytes"
	"fmt"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

func TestPartialFlushKeepsNewVectorSpillsAboveDestinations(t *testing.T) {
	for _, floor := range []int{0, 80} {
		t.Run(fmt.Sprint(floor), func(t *testing.T) {
			f := fn{a: &encoder.Asm{}, s: newStack(), spillFloor: floor, localSlot: []uint32{0}, nLocalSlots: 2}
			f.pushValue(storage{kind: stLocalRef, typ: mtV128})
			for reg := Reg(0); reg < 16; reg++ {
				f.fpinnedLocalMask = f.fpinnedLocalMask.add(reg)
			}
			for reg := Reg(16); reg < 32; reg++ {
				f.pushVReg(reg)
			}
			condition := f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
			f.flushBelow(condition)
			slots := 2 * (1 + 32 - 16)
			var expected encoder.Asm
			expected.StrQ(SP, f.spillOff(max(floor, slots)), Reg(16))
			want := expected.B
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
