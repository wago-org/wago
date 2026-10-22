//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Byte population counts read X6/X7 and return X6/X7. Parallel bit counters
// cannot borrow/carry across their2-,4-,8-bit groups. Both halves finish first.
func (f *integerEmitter) swarPopcnt() {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	f.Constant(12, 0x5555555555555555, true)
	f.Constant(13, 0x3333333333333333, true)
	f.Constant(14, 0x0f0f0f0f0f0f0f0f, true)
	for half, out := range []enc.Reg{enc.X10, enc.X11} {
		src := enc.X6
		if half != 0 {
			src = enc.X7
		}
		f.a.Srli(enc.X28, src, 1)
		f.a.And(enc.X28, enc.X28, enc.X12)
		f.a.Sub(out, src, enc.X28)
		f.a.And(enc.X28, out, enc.X13)
		f.a.Srli(enc.X29, out, 2)
		f.a.And(enc.X29, enc.X29, enc.X13)
		f.a.Add(out, enc.X28, enc.X29)
		f.a.Srli(enc.X28, out, 4)
		f.a.Add(out, out, enc.X28)
		f.a.And(out, out, enc.X14)
	}
	f.a.Addi(enc.X6, enc.X10, 0)
	f.a.Addi(enc.X7, enc.X11, 0)
}

// Q15 multiply and pairwise dot read X5/X6 and X7/X9, returning X5/X6.
// Each signed16 product is widened to64. Dot sums wrap to32; Q15 rounding
// adds16384 before arithmetic shift15 and saturates the sole positive overflow.
func (f *integerEmitter) swarSpecialMultiply(kind uint32) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if kind != 130 && kind != 186 {
		f.err = fmt.Errorf("rv64: invalid SWAR special multiply opcode")
		return
	}
	width := uint8(16)
	if kind == 186 {
		width = 32
	} else {
		f.Constant(12, 16384, true)
		f.Constant(13, 32767, true)
	}
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	for half, out := range []enc.Reg{enc.X10, enc.X11} {
		a, b := enc.X5, enc.X7
		if half != 0 {
			a, b = enc.X6, enc.X9
		}
		for lane := uint8(0); lane < 64/width; lane++ {
			position := lane * width
			f.a.Slli(enc.X28, a, 48-position)
			f.a.Srai(enc.X28, enc.X28, 48)
			f.a.Slli(enc.X29, b, 48-position)
			f.a.Srai(enc.X29, enc.X29, 48)
			if kind == 186 {
				f.a.Mul(enc.Scratch0, enc.X28, enc.X29)
				f.a.Slli(enc.X28, a, 32-position)
				f.a.Srai(enc.X28, enc.X28, 48)
				f.a.Slli(enc.X29, b, 32-position)
				f.a.Srai(enc.X29, enc.X29, 48)
				f.a.Mul(enc.Scratch1, enc.X28, enc.X29)
				f.a.Add(enc.X28, enc.Scratch0, enc.Scratch1)
			} else {
				f.a.Mul(enc.X28, enc.X28, enc.X29)
				f.a.Add(enc.X28, enc.X28, enc.X12)
				f.a.Srai(enc.X28, enc.X28, 15)
				f.a.Slt(enc.Scratch0, enc.X13, enc.X28)
				f.a.Sub(enc.Scratch0, enc.Zero, enc.Scratch0)
				f.a.Xor(enc.Scratch1, enc.X28, enc.X13)
				f.a.And(enc.Scratch1, enc.Scratch1, enc.Scratch0)
				f.a.Xor(enc.X28, enc.X28, enc.Scratch1)
			}
			f.a.Slli(enc.X28, enc.X28, 64-width)
			f.a.Srli(enc.X28, enc.X28, 64-width)
			if position != 0 {
				f.a.Slli(enc.X28, enc.X28, position)
			}
			f.a.Or(out, out, enc.X28)
		}
	}
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}
