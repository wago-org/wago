//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestRelaxedDotLowering(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, borrowed := range []bool{false, true} {
			f := &fn{a: &a64.Asm{}, s: newStack()}
			// Leave only four scratch registers, including the live accumulator.
			for r := Reg(4); r < 32; r++ {
				f.fpinnedLocalMask = f.fpinnedLocalMask.add(r)
			}
			pins := f.fpinnedLocalMask
			n := 2
			if wide {
				n = 3
			}
			for i := 0; i < n; i++ {
				if borrowed {
					f.pushValue(storage{kind: stLocalReg, typ: mtV128, reg: Reg(4 + i)})
				} else {
					f.pushVReg(Reg(i))
				}
			}
			if wide {
				f.i32x4RelaxedDotI8x16I7x16AddS()
			} else {
				f.i16x8RelaxedDotI8x16I7x16S()
			}
			want := []uint32{0x0e20c000, 0x4e20c000, 0x4e60bc00}
			if wide {
				want = []uint32{0x0e20c000, 0x4e20c000, 0x4e602800, 0x4e602800, 0x4ea0bc00, 0x4ea08400}
			}
			var got []uint32
			for i := 0; i < len(f.a.B); i += 4 {
				word := binary.LittleEndian.Uint32(f.a.B[i:])
				if pins.has(Reg(word & 31)) {
					t.Fatalf("wrote pinned local: %08x", word)
				}
				// Ignore operand fields; skip copies from pinned local registers.
				op := word &^ uint32(31|31<<5|31<<16)
				if op == 0x4ea01c00 {
					continue
				}
				got = append(got, op)
			}
			if len(got) != len(want) {
				t.Fatalf("wide=%v borrowed=%v instructions=%x want=%x", wide, borrowed, got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("instruction %d=%08x want=%08x", i, got[i], want[i])
				}
			}
			if f.fpinned != 0 || f.fpinnedLocalMask != pins || f.depth() != 1 {
				t.Fatal("pin or stack state changed")
			}
			result := f.s.back()
			for r, owner := range f.fregUser {
				if Reg(r) == result.st.reg {
					if owner != result {
						t.Fatal("missing result owner")
					}
				} else if owner != nil {
					t.Fatalf("leaked register %d", r)
				}
			}
		}
	}
}
