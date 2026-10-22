//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// R=0 interprets both byte operands as signed and saturates each pair sum to
// signed16. The product-pair range is [-32512,32768], so only positive overflow
// needs clipping. Dot-add sign-extends two saturated pair sums, then adds the
// third input modulo32; summing four unsaturated products would be incorrect.
// Fixed pairs5/6,7/9 and(optional)12/13 survive until output10/11 commits.
func (f *integerEmitter) swarRelaxedDot(kind uint32) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if kind != 274 && kind != 275 {
		f.err = fmt.Errorf("rv64: invalid SWAR relaxed dot opcode")
		return
	}
	width, pairs := uint8(16), uint8(1)
	if kind == 275 {
		width, pairs = 32, 2
	}
	f.Constant(14, 32767, true)
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	for half, out := range []enc.Reg{enc.X10, enc.X11} {
		a, b, c := enc.X5, enc.X7, enc.X12
		if half != 0 {
			a, b, c = enc.X6, enc.X9, enc.X13
		}
		for lane := uint8(0); lane < 64/width; lane++ {
			position := lane * width
			f.a.Addi(enc.X16, enc.Zero, 0)
			for pair := uint8(0); pair < pairs; pair++ {
				start := position + 16*pair
				f.a.Slli(enc.X28, a, 56-start)
				f.a.Srai(enc.X28, enc.X28, 56)
				f.a.Slli(enc.X29, b, 56-start)
				f.a.Srai(enc.X29, enc.X29, 56)
				f.a.Mul(enc.Scratch0, enc.X28, enc.X29)
				f.a.Slli(enc.X28, a, 48-start)
				f.a.Srai(enc.X28, enc.X28, 56)
				f.a.Slli(enc.X29, b, 48-start)
				f.a.Srai(enc.X29, enc.X29, 56)
				f.a.Mul(enc.Scratch1, enc.X28, enc.X29)
				f.a.Add(enc.X28, enc.Scratch0, enc.Scratch1)
				f.a.Slt(enc.Scratch0, enc.X14, enc.X28)
				f.a.Sub(enc.Scratch0, enc.Zero, enc.Scratch0)
				f.a.Xor(enc.Scratch1, enc.X28, enc.X14)
				f.a.And(enc.Scratch1, enc.Scratch1, enc.Scratch0)
				f.a.Xor(enc.X28, enc.X28, enc.Scratch1)
				f.a.Add(enc.X16, enc.X16, enc.X28)
			}
			if kind == 275 {
				f.a.Slli(enc.X28, c, 32-position)
				f.a.Srli(enc.X28, enc.X28, 32)
				f.a.Add(enc.X16, enc.X16, enc.X28)
			}
			f.a.Slli(enc.X16, enc.X16, 64-width)
			f.a.Srli(enc.X16, enc.X16, 64-width)
			if position != 0 {
				f.a.Slli(enc.X16, enc.X16, position)
			}
			f.a.Or(out, out, enc.X16)
		}
	}
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}
