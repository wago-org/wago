//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Fixed inputs X5/X6 and X7/X9 survive until both output halves commit.
// Ordinary min/max replace any NaN with canonical NaN and use FMIN/FMAX for
// signed zero. Pseudo min/max select the second raw operand only on a strict
// ordered comparison, preserving the first operand on equality or unordered.
func (f *integerEmitter) swarFloatMinMax(width uint8, maximum, pseudo bool) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if width != 32 && width != 64 {
		f.err = fmt.Errorf("rv64: invalid SWAR floating min/max width")
		return
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
			f.a.Slli(enc.X28, a, 64-width-position)
			f.a.Srli(enc.X28, enc.X28, 64-width)
			f.a.Slli(enc.X29, b, 64-width-position)
			f.a.Srli(enc.X29, enc.X29, 64-width)
			f.floatInput(enc.F0, enc.X28, width == 64)
			f.floatInput(enc.F1, enc.X29, width == 64)
			if pseudo {
				left, right := enc.F1, enc.F0
				if maximum {
					left, right = right, left
				}
				if width == 64 {
					f.a.FltD(enc.Scratch0, left, right)
				} else {
					f.a.FltS(enc.Scratch0, left, right)
				}
				f.a.Sub(enc.Scratch0, enc.Zero, enc.Scratch0)
				f.a.Xor(enc.Scratch1, enc.X28, enc.X29)
				f.a.And(enc.Scratch1, enc.Scratch1, enc.Scratch0)
				f.a.Xor(enc.Scratch0, enc.X28, enc.Scratch1)
			} else {
				f.floatClass(enc.X12, enc.F0, width == 64)
				f.floatClass(enc.X13, enc.F1, width == 64)
				f.a.Or(enc.X12, enc.X12, enc.X13)
				f.a.Addi(enc.X13, enc.Zero, 0x300)
				f.a.And(enc.X12, enc.X12, enc.X13)
				f.a.Sltu(enc.X12, enc.Zero, enc.X12)
				f.a.Sub(enc.X12, enc.Zero, enc.X12)
				if width == 64 {
					if maximum {
						f.a.FmaxD(enc.F0, enc.F0, enc.F1)
					} else {
						f.a.FminD(enc.F0, enc.F0, enc.F1)
					}
					f.a.FmvXD(enc.Scratch0, enc.F0)
					f.a.Constant64(enc.X13, 0x7ff8000000000000)
				} else {
					if maximum {
						f.a.FmaxS(enc.F0, enc.F0, enc.F1)
					} else {
						f.a.FminS(enc.F0, enc.F0, enc.F1)
					}
					f.a.FmvXW(enc.Scratch0, enc.F0)
					f.a.ZeroExtend32(enc.Scratch0, enc.Scratch0)
					f.a.Constant64(enc.X13, 0x7fc00000)
				}
				f.a.Xor(enc.X13, enc.X13, enc.Scratch0)
				f.a.And(enc.X13, enc.X13, enc.X12)
				f.a.Xor(enc.Scratch0, enc.Scratch0, enc.X13)
			}
			if position != 0 {
				f.a.Slli(enc.Scratch0, enc.Scratch0, position)
			}
			f.a.Or(out, out, enc.Scratch0)
		}
	}
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}

func swarFloatMinMaxOpcode(kind uint32) (width uint8, maximum, pseudo, ok bool) {
	if kind >= 232 && kind <= 235 {
		return 32, kind&1 != 0, kind >= 234, true
	}
	if kind >= 244 && kind <= 247 {
		return 64, kind&1 != 0, kind >= 246, true
	}
	return 0, false, false, false
}
