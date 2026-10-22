//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Extmultiply consumes a selected half of each fixed X5/X6 and X7/X9 pair.
// Signed or unsigned products fit the doubled lane width, including full64.
func (f *integerEmitter) swarExtmultiply(width uint8, signed, high bool) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if width != 8 && width != 16 && width != 32 {
		f.err = fmt.Errorf("rv64: invalid SWAR extmultiply width")
		return
	}
	a, b := enc.X5, enc.X7
	if high {
		a, b = enc.X6, enc.X9
	}
	resultWidth := width * 2
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	for half, out := range []enc.Reg{enc.X10, enc.X11} {
		for lane := uint8(0); lane < 64/resultWidth; lane++ {
			position := (uint8(half)*(64/resultWidth) + lane) * width
			f.a.Slli(enc.X28, a, 64-width-position)
			f.a.Slli(enc.X29, b, 64-width-position)
			if signed {
				f.a.Srai(enc.X28, enc.X28, 64-width)
				f.a.Srai(enc.X29, enc.X29, 64-width)
			} else {
				f.a.Srli(enc.X28, enc.X28, 64-width)
				f.a.Srli(enc.X29, enc.X29, 64-width)
			}
			f.a.Mul(enc.X28, enc.X28, enc.X29)
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
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}

// Narrow concatenates the saturated lanes of both fixed input vectors into
// X5/X6. Source lanes are signed even when the result saturation is unsigned.
func (f *integerEmitter) swarNarrow(width uint8, signed bool) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if width != 8 && width != 16 {
		f.err = fmt.Errorf("rv64: invalid SWAR narrowing width")
		return
	}
	sourceWidth := width * 2
	low, high := int64(0), int64(1<<width)-1
	if signed {
		low, high = -int64(1<<(width-1)), int64(1<<(width-1))-1
	}
	f.Constant(12, low, true)
	f.Constant(13, high, true)
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	for half, out := range []enc.Reg{enc.X10, enc.X11} {
		lo, hi := enc.X5, enc.X6
		if half != 0 {
			lo, hi = enc.X7, enc.X9
		}
		for lane := uint8(0); lane < 64/width; lane++ {
			src := lo
			if lane >= 64/sourceWidth {
				src = hi
			}
			position := (lane % (64 / sourceWidth)) * sourceWidth
			f.a.Slli(enc.X28, src, 64-sourceWidth-position)
			f.a.Srai(enc.X28, enc.X28, 64-sourceWidth)
			f.a.Slt(enc.Scratch0, enc.X28, enc.X12)
			f.a.Sub(enc.Scratch0, enc.Zero, enc.Scratch0)
			f.a.Xor(enc.Scratch1, enc.X28, enc.X12)
			f.a.And(enc.Scratch1, enc.Scratch1, enc.Scratch0)
			f.a.Xor(enc.X28, enc.X28, enc.Scratch1)
			f.a.Slt(enc.Scratch0, enc.X13, enc.X28)
			f.a.Sub(enc.Scratch0, enc.Zero, enc.Scratch0)
			f.a.Xor(enc.Scratch1, enc.X28, enc.X13)
			f.a.And(enc.Scratch1, enc.Scratch1, enc.Scratch0)
			f.a.Xor(enc.X28, enc.X28, enc.Scratch1)
			f.a.Slli(enc.X28, enc.X28, 64-width)
			f.a.Srli(enc.X28, enc.X28, 64-width)
			if lane != 0 {
				f.a.Slli(enc.X28, enc.X28, lane*width)
			}
			f.a.Or(out, out, enc.X28)
		}
	}
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}

func swarExtmultiplyOpcode(kind uint32) (width uint8, signed, high, ok bool) {
	if (kind >= 156 && kind <= 159) || (kind >= 188 && kind <= 191) || (kind >= 220 && kind <= 223) {
		offset := (kind - 156) % 32
		return uint8(8 << ((kind - 156) / 32)), offset < 2, offset%2 != 0, true
	}
	return 0, false, false, false
}

func swarNarrowOpcode(kind uint32) (width uint8, signed, ok bool) {
	switch kind {
	case 101:
		return 8, true, true
	case 102:
		return 8, false, true
	case 133:
		return 16, true, true
	case 134:
		return 16, false, true
	default:
		return 0, false, false
	}
}
