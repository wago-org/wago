//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarProduct uint8

const (
	swarMul swarProduct = iota
	swarMinS
	swarMinU
	swarMaxS
	swarMaxU
)

// Product/minmax reads X5/X6 and X7/X9, returning X5/X6. X28/X29 isolate
// each lane, reserved scratches hold arithmetic/selection, and X10/X11 keep
// both output halves until the inputs have been completely consumed.
func (f *integerEmitter) swarProduct(width uint8, operation swarProduct) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if (width != 8 && width != 16 && width != 32 && width != 64) || operation > swarMaxU ||
		(width == 64 && operation != swarMul) || (width == 8 && operation == swarMul) {
		f.err = fmt.Errorf("rv64: invalid SWAR multiply/minmax width/operation")
		return
	}
	signed := operation == swarMinS || operation == swarMaxS
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
			f.a.Slli(enc.X29, b, 64-width-position)
			if signed {
				f.a.Srai(enc.X28, enc.X28, 64-width)
				f.a.Srai(enc.X29, enc.X29, 64-width)
			} else {
				f.a.Srli(enc.X28, enc.X28, 64-width)
				f.a.Srli(enc.X29, enc.X29, 64-width)
			}
			if operation == swarMul {
				f.a.Mul(enc.Scratch0, enc.X28, enc.X29)
			} else {
				if signed {
					f.a.Slt(enc.Scratch0, enc.X28, enc.X29)
				} else {
					f.a.Sltu(enc.Scratch0, enc.X28, enc.X29)
				}
				f.a.Sub(enc.Scratch0, enc.Zero, enc.Scratch0)
				f.a.Xor(enc.Scratch1, enc.X28, enc.X29)
				f.a.And(enc.Scratch1, enc.Scratch1, enc.Scratch0)
				base := enc.X29 // min: a if a<b, otherwise b
				if operation == swarMaxS || operation == swarMaxU {
					base = enc.X28 // max: b if a<b, otherwise a
				}
				f.a.Xor(enc.Scratch0, base, enc.Scratch1)
			}
			if width != 64 {
				f.a.Slli(enc.Scratch0, enc.Scratch0, 64-width)
				f.a.Srli(enc.Scratch0, enc.Scratch0, 64-width)
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

func swarProductOpcode(kind uint32) (width uint8, operation swarProduct, ok bool) {
	switch {
	case kind >= 118 && kind <= 121:
		return 8, swarProduct(kind - 118 + uint32(swarMinS)), true
	case kind >= 150 && kind <= 153:
		return 16, swarProduct(kind - 150 + uint32(swarMinS)), true
	case kind >= 182 && kind <= 185:
		return 32, swarProduct(kind - 182 + uint32(swarMinS)), true
	}
	switch kind {
	case 149:
		return 16, swarMul, true
	case 181:
		return 32, swarMul, true
	case 213:
		return 64, swarMul, true
	default:
		return 0, 0, false
	}
}
