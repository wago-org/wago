//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Preserve both input halves in caller-clobbered X14/X15 while the scalar
// routine uses X5/X6/X7 and F0/F1/F2. Its large-value path retains raw X5,
// its NaN path canonicalizes, and sign injection preserves negative zero.
// Complete output halves X10/X11 are committed only after all lanes finish.
func (f *integerEmitter) swarFloatRound(width uint8, mode enc.RoundingMode) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if (width != 32 && width != 64) || mode > enc.RUP {
		f.err = fmt.Errorf("rv64: invalid SWAR floating rounding width/mode")
		return
	}
	f.a.Addi(enc.X14, enc.X5, 0)
	f.a.Addi(enc.X15, enc.X6, 0)
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	for half, out := range []enc.Reg{enc.X10, enc.X11} {
		src := enc.X14
		if half != 0 {
			src = enc.X15
		}
		for lane := uint8(0); lane < 64/width; lane++ {
			position := lane * width
			f.a.Slli(enc.X5, src, 64-width-position)
			f.a.Srli(enc.X5, enc.X5, 64-width)
			f.floatInput(enc.F0, enc.X5, width == 64)
			f.floatRound(width == 64, mode)
			if position != 0 {
				f.a.Slli(enc.X5, enc.X5, position)
			}
			f.a.Or(out, out, enc.X5)
		}
	}
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}

func swarFloatRoundOpcode(kind uint32) (width uint8, mode enc.RoundingMode, ok bool) {
	switch kind {
	case 103:
		return 32, enc.RUP, true
	case 104:
		return 32, enc.RDN, true
	case 105:
		return 32, enc.RTZ, true
	case 106:
		return 32, enc.RNE, true
	case 116:
		return 64, enc.RUP, true
	case 117:
		return 64, enc.RDN, true
	case 122:
		return 64, enc.RTZ, true
	case 148:
		return 64, enc.RNE, true
	default:
		return 0, 0, false
	}
}
