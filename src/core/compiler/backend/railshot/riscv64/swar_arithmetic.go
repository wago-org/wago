//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Wrapping lane arithmetic reads X5/X6 and X7/X9, returning X5/X6.
// X10/X11 hold repeated guard/low masks; X28/X29 and both reserved scratches
// are temporary. Both result halves are computed before overwriting inputs.
func (f *integerEmitter) swarAddSub(width uint8, subtract bool) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if width != 8 && width != 16 && width != 32 && width != 64 {
		f.err = fmt.Errorf("rv64: invalid SWAR arithmetic lane width")
		return
	}
	if width == 64 {
		if subtract {
			f.a.Sub(enc.Scratch0, enc.X5, enc.X7)
			f.a.Sub(enc.Scratch1, enc.X6, enc.X9)
		} else {
			f.a.Add(enc.Scratch0, enc.X5, enc.X7)
			f.a.Add(enc.Scratch1, enc.X6, enc.X9)
		}
	} else {
		var guard uint64
		for shift := uint8(0); shift < 64; shift += width {
			guard |= uint64(1) << (shift + width - 1)
		}
		f.a.Constant64(enc.X10, int64(guard))
		f.a.Xori(enc.X11, enc.X10, -1)
		for half, out := range []enc.Reg{enc.Scratch0, enc.Scratch1} {
			a, b := enc.X5, enc.X7
			if half != 0 {
				a, b = enc.X6, enc.X9
			}
			if subtract {
				// Set every minuend guard and clear every subtrahend guard.
				// Each lane's intermediate difference is positive, preventing
				// borrow propagation. Its guard is 1 XOR the local borrow.
				f.a.Or(enc.X28, a, enc.X10)
				f.a.And(enc.X29, b, enc.X11)
				f.a.Sub(out, enc.X28, enc.X29)
			} else {
				// Two lane lows sum to at most 2^width-2, so the carry can
				// enter this lane's guard but cannot cross into the next lane.
				f.a.And(enc.X28, a, enc.X11)
				f.a.And(enc.X29, b, enc.X11)
				f.a.Add(out, enc.X28, enc.X29)
			}
			f.a.Xor(enc.X28, a, b)
			f.a.And(enc.X28, enc.X28, enc.X10)
			if subtract {
				f.a.Xor(enc.X28, enc.X28, enc.X10)
			}
			f.a.Xor(out, out, enc.X28)
		}
	}
	f.a.Addi(enc.X5, enc.Scratch0, 0)
	f.a.Addi(enc.X6, enc.Scratch1, 0)
}

func swarAddSubOpcode(kind uint32) (width uint8, subtract, ok bool) {
	switch kind {
	case 110, 113:
		return 8, kind == 113, true
	case 142, 145:
		return 16, kind == 145, true
	case 174, 177:
		return 32, kind == 177, true
	case 206, 209:
		return 64, kind == 209, true
	default:
		return 0, false, false
	}
}
