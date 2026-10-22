//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarFloatMath uint8

const (
	swarFloatAbs swarFloatMath = iota
	swarFloatNeg
	swarFloatSqrt
	swarFloatAdd
	swarFloatSub
	swarFloatMul
	swarFloatDiv
)

// Fixed pairs X5/X6 and X7/X9 remain live until complete outputs X10/X11
// commit. Sign operations preserve raw payloads in GP registers; arithmetic
// uses caller-clobbered F0/F1 and staticRNE regardless of ambient FCSR.
func (f *integerEmitter) swarFloatMath(width uint8, operation swarFloatMath) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if (width != 32 && width != 64) || operation > swarFloatDiv {
		f.err = fmt.Errorf("rv64: invalid SWAR floating arithmetic width/operation")
		return
	}
	if operation == swarFloatAbs || operation == swarFloatNeg {
		sign := uint64(0x8000000080000000)
		if width == 64 {
			sign = 0x8000000000000000
		}
		if operation == swarFloatAbs {
			f.a.Constant64(enc.Scratch0, int64(^sign))
			f.a.And(enc.X10, enc.X5, enc.Scratch0)
			f.a.And(enc.X11, enc.X6, enc.Scratch0)
		} else {
			f.a.Constant64(enc.Scratch0, int64(sign))
			f.a.Xor(enc.X10, enc.X5, enc.Scratch0)
			f.a.Xor(enc.X11, enc.X6, enc.Scratch0)
		}
	} else {
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
				f.floatInput(enc.F0, enc.X28, width == 64)
				if operation >= swarFloatAdd {
					f.a.Slli(enc.X29, b, 64-width-position)
					f.a.Srli(enc.X29, enc.X29, 64-width)
					f.floatInput(enc.F1, enc.X29, width == 64)
				}
				if width == 64 {
					switch operation {
					case swarFloatSqrt:
						f.a.FsqrtD(enc.F0, enc.F0, enc.RNE)
					case swarFloatAdd:
						f.a.FaddD(enc.F0, enc.F0, enc.F1, enc.RNE)
					case swarFloatSub:
						f.a.FsubD(enc.F0, enc.F0, enc.F1, enc.RNE)
					case swarFloatMul:
						f.a.FmulD(enc.F0, enc.F0, enc.F1, enc.RNE)
					case swarFloatDiv:
						f.a.FdivD(enc.F0, enc.F0, enc.F1, enc.RNE)
					}
					f.a.FmvXD(enc.Scratch0, enc.F0)
				} else {
					switch operation {
					case swarFloatSqrt:
						f.a.FsqrtS(enc.F0, enc.F0, enc.RNE)
					case swarFloatAdd:
						f.a.FaddS(enc.F0, enc.F0, enc.F1, enc.RNE)
					case swarFloatSub:
						f.a.FsubS(enc.F0, enc.F0, enc.F1, enc.RNE)
					case swarFloatMul:
						f.a.FmulS(enc.F0, enc.F0, enc.F1, enc.RNE)
					case swarFloatDiv:
						f.a.FdivS(enc.F0, enc.F0, enc.F1, enc.RNE)
					}
					f.a.FmvXW(enc.Scratch0, enc.F0)
					f.a.ZeroExtend32(enc.Scratch0, enc.Scratch0)
				}
				if position != 0 {
					f.a.Slli(enc.Scratch0, enc.Scratch0, position)
				}
				f.a.Or(out, out, enc.Scratch0)
			}
		}
	}
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}

func swarFloatMathOpcode(kind uint32) (width uint8, operation swarFloatMath, unary, ok bool) {
	width = 32
	if kind >= 236 {
		width = 64
		kind -= 12
	}
	switch kind {
	case 224:
		return width, swarFloatAbs, true, true
	case 225:
		return width, swarFloatNeg, true, true
	case 227:
		return width, swarFloatSqrt, true, true
	case 228:
		return width, swarFloatAdd, false, true
	case 229:
		return width, swarFloatSub, false, true
	case 230:
		return width, swarFloatMul, false, true
	case 231:
		return width, swarFloatDiv, false, true
	default:
		return 0, 0, false, false
	}
}
