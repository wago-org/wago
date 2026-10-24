//go:build arm64

package arm64

import (
	"bytes"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

func TestRegMoveCursorWindowEmission(t *testing.T) {
	for _, floating := range []bool{false, true} {
		registers := make([]Reg, 0, 30)
		for i := 0; i < 30; i++ {
			if floating || i != 16 {
				registers = append(registers, Reg(i))
			}
		}
		cycle := make([]regMove, 0, len(registers)+1)
		for i, r := range registers {
			cycle = append(cycle, regMove{dst: r, src: registers[(i+1)%len(registers)]})
		}
		// One ordinary move shifts a long swap sequence across the 24-op flush.
		cycle = append(cycle, regMove{dst: 30, src: 31})
		fanout := make([]regMove, 0, 31)
		for i := 0; i < 31; i++ {
			if floating || i != 16 {
				fanout = append(fanout, regMove{dst: Reg(i), src: 31})
			}
		}
		for _, moves := range [][]regMove{nil, {{dst: 0, src: 0}}, fanout, cycle} {
			old := fn{a: &encoder.Asm{}, s: newStack()}
			actual := fn{a: &encoder.Asm{}, s: newStack()}
			for round := 0; round < 2; round++ {
				// Changing the live-prefix floor catches reuse of an old FP swap slot.
				old.spillFloor = round * 4
				actual.spillFloor = round * 4
				swapSlot := -1
				slot := func() int32 {
					if swapSlot < 0 {
						swapSlot = old.allocSpillSlot()
					}
					return old.spillOff(swapSlot)
				}
				move := func(dst, src Reg) {
					if floating {
						old.a.FmovReg(dst, src, true)
					} else {
						old.a.MovReg64(dst, src)
					}
				}
				swap := func(a, b Reg) {
					if floating {
						off := slot()
						old.fst(SP, off, a, true)
						old.a.FmovReg(a, b, true)
						old.fld(b, SP, off, true)
					} else {
						old.a.MovReg64(X16, a)
						old.a.MovReg64(a, b)
						old.a.MovReg64(b, X16)
					}
				}
				chain := func(a, b, c Reg) {
					if floating {
						off := slot()
						old.fst(SP, off, a, true)
						old.a.FmovReg(a, b, true)
						old.a.FmovReg(b, c, true)
						old.fld(c, SP, off, true)
					} else {
						old.a.MovReg64(X16, a)
						old.a.MovReg64(a, b)
						old.a.MovReg64(b, c)
						old.a.MovReg64(c, X16)
					}
				}
				want := resolveRegMovesWindow(moves, move, swap, chain)
				got := actual.emitRegMovesWindow(moves, floating)
				if got != want || old.maxSpill != actual.maxSpill || !bytes.Equal(old.a.B, actual.a.B) {
					t.Fatalf("floating=%v round=%d moves=%d: cursor changed bytes, swap rewrites or spill extent", floating, round, len(moves))
				}
			}
		}
	}
}
