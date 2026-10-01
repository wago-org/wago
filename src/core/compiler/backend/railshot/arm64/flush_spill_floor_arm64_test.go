//go:build arm64

package arm64

import (
	"bytes"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestFlushReservesCanonicalSlotsBeforeVectorSpillARM64(t *testing.T) {
	for _, floor := range []int{0, 40} {
		f := &fn{a: &a64.Asm{}, s: newStack(), spillFloor: floor, localSlot: []uint32{0}, nLocalSlots: 2}
		// Materializing the first root must spill a later vector. Sixteen local
		// pins and sixteen live vectors fill the SIMD bank below the wide-flush
		// threshold, so that spill must avoid all 34 canonical destination slots.
		f.pushValue(storage{kind: stLocalRef, typ: mtV128})
		for r := Reg(0); r < 16; r++ {
			f.fpinnedLocalMask = f.fpinnedLocalMask.add(r)
		}
		for r := Reg(16); r < 32; r++ {
			f.pushVReg(r)
		}
		f.flush()
		var want a64.Asm
		want.StrQ(SP, f.spillOff(max(floor, 34)), 16)
		if !bytes.HasPrefix(f.a.B, want.B) {
			t.Fatalf("first spill = %x, want %x above canonical destinations", f.a.B[:4], want.B)
		}
		if f.spillFloor != floor {
			t.Fatalf("spill floor = %d, want restored %d", f.spillFloor, floor)
		}
		roots := f.rootsBottomToTop()
		if len(roots) != 17 || f.maxSpill < max(floor, 34)+2 {
			t.Fatalf("root count / frame slots = %d / %d", len(roots), f.maxSpill)
		}
		for i, root := range roots {
			if root.st.kind != stSlot || root.st.typ != mtV128 || root.st.slot != uint32(2*i) {
				t.Fatalf("root %d = %+v, want canonical vector", i, root.st)
			}
		}
	}
}

func BenchmarkFlushCanonicalSlotsARM64(b *testing.B) {
	for _, vectors := range []bool{false, true} {
		name := "scalar"
		if vectors {
			name = "vector"
		}
		b.Run(name, func(b *testing.B) {
			f := &fn{a: &a64.Asm{B: make([]byte, 0, 1024)}, s: newStack(), localSlot: []uint32{0}, nLocalSlots: 2}
			if vectors {
				for r := Reg(0); r < 15; r++ {
					f.fpinnedLocalMask = f.fpinnedLocalMask.add(r)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				f.a.B = f.a.B[:0]
				f.s.reset()
				if vectors {
					f.pushValue(storage{kind: stLocalRef, typ: mtV128})
					for r := Reg(16); r < 32; r++ {
						f.pushVReg(r)
					}
				} else {
					for i := range 16 {
						f.pushValue(storage{kind: stConst, typ: mtI64, cval: int64(i)})
					}
				}
				f.flush()
			}
		})
	}
}
