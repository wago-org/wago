//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Widen reads X6/X7 and returns X6/X7, staging output halves in X10/X11.
// Extension selects one source half; pairwise-add consumes both source halves.
func (f *integerEmitter) swarWiden(width uint8, signed, high, pairwise bool) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if (width != 8 && width != 16 && width != 32) || (pairwise && (width == 32 || high)) {
		f.err = fmt.Errorf("rv64: invalid SWAR widening width/mode")
		return
	}
	resultWidth := width * 2
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	for half, out := range []enc.Reg{enc.X10, enc.X11} {
		src := enc.X6
		if high || (pairwise && half != 0) {
			src = enc.X7
		}
		for lane := uint8(0); lane < 64/resultWidth; lane++ {
			position := (uint8(half)*(64/resultWidth) + lane) * width
			if pairwise {
				position = lane * resultWidth
			}
			f.a.Slli(enc.X28, src, 64-width-position)
			if signed {
				f.a.Srai(enc.X28, enc.X28, 64-width)
			} else {
				f.a.Srli(enc.X28, enc.X28, 64-width)
			}
			if pairwise {
				f.a.Slli(enc.X29, src, 64-2*width-position)
				if signed {
					f.a.Srai(enc.X29, enc.X29, 64-width)
				} else {
					f.a.Srli(enc.X29, enc.X29, 64-width)
				}
				f.a.Add(enc.X28, enc.X28, enc.X29)
			}
			if resultWidth != 64 {
				f.a.Slli(enc.X28, enc.X28, 64-resultWidth)
				f.a.Srli(enc.X28, enc.X28, 64-resultWidth)
			}
			if lane != 0 {
				f.a.Slli(enc.X28, enc.X28, lane*resultWidth)
			}
			f.a.Or(out, out, enc.X28)
		}
	}
	f.a.Addi(enc.X6, enc.X10, 0)
	f.a.Addi(enc.X7, enc.X11, 0)
}

func swarWidenOpcode(kind uint32) (width uint8, signed, high, pairwise, ok bool) {
	if kind >= 124 && kind <= 127 {
		return uint8(8 << ((kind - 124) / 2)), (kind-124)%2 == 0, false, true, true
	}
	if (kind >= 135 && kind <= 138) || (kind >= 167 && kind <= 170) || (kind >= 199 && kind <= 202) {
		offset := (kind - 135) % 32
		return uint8(8 << ((kind - 135) / 32)), offset < 2, offset%2 != 0, false, true
	}
	return 0, false, false, false, false
}
