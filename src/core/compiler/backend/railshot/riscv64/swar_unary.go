//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarUnary uint8

const (
	swarAbs swarUnary = iota
	swarNeg
	swarAllTrue
	swarBitmask
)

// Unary reads X6/X7, returning X6/X7 for abs/neg or normalized I32 X5 for
// reductions. X28/X29 are temporary, and X10/X11 accumulate vector output.
func (f *integerEmitter) swarUnary(width uint8, operation swarUnary) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if (width != 8 && width != 16 && width != 32 && width != 64) || operation > swarBitmask {
		f.err = fmt.Errorf("rv64: invalid SWAR unary width/operation")
		return
	}
	if operation == swarAllTrue {
		f.a.Addi(enc.X5, enc.Zero, 1)
	} else if operation == swarBitmask {
		f.a.Addi(enc.X5, enc.Zero, 0)
		f.a.Addi(enc.X29, enc.Zero, 1)
	} else {
		f.a.Addi(enc.X10, enc.Zero, 0)
		f.a.Addi(enc.X11, enc.Zero, 0)
	}
	for half, out := range []enc.Reg{enc.X10, enc.X11} {
		src := enc.X6
		if half != 0 {
			src = enc.X7
		}
		for lane := uint8(0); lane < 64/width; lane++ {
			position := lane * width
			if operation == swarBitmask {
				f.a.Srli(enc.X28, src, position+width-1)
				f.a.And(enc.X28, enc.X28, enc.X29)
				index := uint8(half)*(64/width) + lane
				if index != 0 {
					f.a.Slli(enc.X28, enc.X28, index)
				}
				f.a.Or(enc.X5, enc.X5, enc.X28)
				continue
			}
			f.a.Slli(enc.X28, src, 64-width-position)
			if operation == swarAbs {
				f.a.Srai(enc.X28, enc.X28, 64-width)
			} else {
				f.a.Srli(enc.X28, enc.X28, 64-width)
			}
			if operation == swarAllTrue {
				f.a.Sltu(enc.X28, enc.Zero, enc.X28)
				f.a.And(enc.X5, enc.X5, enc.X28)
				continue
			}
			if operation == swarAbs {
				// (value XOR sign) - sign wraps at signed minima as Wasm does.
				f.a.Srai(enc.X29, enc.X28, 63)
				f.a.Xor(enc.X28, enc.X28, enc.X29)
				f.a.Sub(enc.X28, enc.X28, enc.X29)
			} else {
				f.a.Sub(enc.X28, enc.Zero, enc.X28)
			}
			if width != 64 {
				f.a.Slli(enc.X28, enc.X28, 64-width)
				f.a.Srli(enc.X28, enc.X28, 64-width)
			}
			if position != 0 {
				f.a.Slli(enc.X28, enc.X28, position)
			}
			f.a.Or(out, out, enc.X28)
		}
	}
	if operation == swarAbs || operation == swarNeg {
		f.a.Addi(enc.X6, enc.X10, 0)
		f.a.Addi(enc.X7, enc.X11, 0)
	}
}

func swarUnaryOpcode(kind uint32) (width uint8, operation swarUnary, ok bool) {
	switch kind {
	case 96, 128, 160, 192:
		operation = swarAbs
	case 97, 129, 161, 193:
		operation = swarNeg
	case 99, 131, 163, 195:
		operation = swarAllTrue
	case 100, 132, 164, 196:
		operation = swarBitmask
	default:
		return 0, 0, false
	}
	return uint8(8 << ((kind - 96) / 32)), operation, true
}
