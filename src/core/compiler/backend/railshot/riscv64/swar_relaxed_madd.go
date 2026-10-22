//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Fixed inputs X5/X6, X7/X9 and X12/X13 remain live until both X10/X11
// outputs commit. R=0 is explicitly unfused multiplication then addition.
// NMADD negates the first operand before multiplication, including zero sign.
func (f *integerEmitter) swarRelaxedMadd(width uint8, negative bool) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if width != 32 && width != 64 {
		f.err = fmt.Errorf("rv64: invalid SWAR relaxed multiply-add width")
		return
	}
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	for half, out := range []enc.Reg{enc.X10, enc.X11} {
		a, b, c := enc.X5, enc.X7, enc.X12
		if half != 0 {
			a, b, c = enc.X6, enc.X9, enc.X13
		}
		for lane := uint8(0); lane < 64/width; lane++ {
			position := lane * width
			f.a.Slli(enc.X28, a, 64-width-position)
			f.a.Srli(enc.X28, enc.X28, 64-width)
			f.a.Slli(enc.X29, b, 64-width-position)
			f.a.Srli(enc.X29, enc.X29, 64-width)
			f.a.Slli(enc.Scratch0, c, 64-width-position)
			f.a.Srli(enc.Scratch0, enc.Scratch0, 64-width)
			f.floatInput(enc.F0, enc.X28, width == 64)
			f.floatInput(enc.F1, enc.X29, width == 64)
			f.floatInput(enc.F2, enc.Scratch0, width == 64)
			if width == 64 {
				if negative {
					f.a.FsgnjnD(enc.F0, enc.F0, enc.F0)
				}
				f.a.FmulD(enc.F0, enc.F0, enc.F1, enc.RNE)
				f.a.FaddD(enc.F0, enc.F0, enc.F2, enc.RNE)
				f.a.FmvXD(enc.Scratch0, enc.F0)
			} else {
				if negative {
					f.a.FsgnjnS(enc.F0, enc.F0, enc.F0)
				}
				f.a.FmulS(enc.F0, enc.F0, enc.F1, enc.RNE)
				f.a.FaddS(enc.F0, enc.F0, enc.F2, enc.RNE)
				f.a.FmvXW(enc.Scratch0, enc.F0)
				f.a.ZeroExtend32(enc.Scratch0, enc.Scratch0)
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

func swarRelaxedMaddOpcode(kind uint32) (width uint8, negative, ok bool) {
	if kind >= 261 && kind <= 264 {
		width = 32
		if kind >= 263 {
			width = 64
		}
		return width, kind%2 == 0, true
	}
	return 0, false, false
}
