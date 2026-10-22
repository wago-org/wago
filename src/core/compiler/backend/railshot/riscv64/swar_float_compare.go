//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarFloatRelation uint8

const (
	swarFloatEQ swarFloatRelation = iota
	swarFloatNE
	swarFloatLT
	swarFloatGT
	swarFloatLE
	swarFloatGE
)

// Read fixed vector pairs X5/X6, X7/X9; stage complete mask halves X10/X11
// before returning X5/X6. F0/F1 are caller-clobbered FP scratch. FMV.W.X
// NaN-boxes raw f32 lanes; FEQ/FLT/FLE supply Wasm unordered comparison rules.
func (f *integerEmitter) swarFloatCompare(width uint8, relation swarFloatRelation) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if (width != 32 && width != 64) || relation > swarFloatGE {
		f.err = fmt.Errorf("rv64: invalid SWAR floating comparison width/relation")
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
			if width == 32 {
				f.a.FmvWX(enc.F0, enc.X28)
				f.a.FmvWX(enc.F1, enc.X29)
			} else {
				f.a.FmvDX(enc.F0, enc.X28)
				f.a.FmvDX(enc.F1, enc.X29)
			}
			left, right := enc.F0, enc.F1
			if relation == swarFloatGT || relation == swarFloatGE {
				left, right = right, left
			}
			switch relation {
			case swarFloatEQ, swarFloatNE:
				if width == 32 {
					f.a.FeqS(enc.Scratch0, left, right)
				} else {
					f.a.FeqD(enc.Scratch0, left, right)
				}
				if relation == swarFloatNE {
					f.a.Xori(enc.Scratch0, enc.Scratch0, 1)
				}
			case swarFloatLT, swarFloatGT:
				if width == 32 {
					f.a.FltS(enc.Scratch0, left, right)
				} else {
					f.a.FltD(enc.Scratch0, left, right)
				}
			case swarFloatLE, swarFloatGE:
				if width == 32 {
					f.a.FleS(enc.Scratch0, left, right)
				} else {
					f.a.FleD(enc.Scratch0, left, right)
				}
			}
			f.a.Sub(enc.Scratch0, enc.Zero, enc.Scratch0)
			if width == 32 {
				f.a.Slli(enc.Scratch0, enc.Scratch0, 32)
				f.a.Srli(enc.Scratch0, enc.Scratch0, 32)
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

func swarFloatComparisonOpcode(kind uint32) (width uint8, relation swarFloatRelation, ok bool) {
	if kind >= 65 && kind <= 70 {
		return 32, swarFloatRelation(kind - 65), true
	}
	if kind >= 71 && kind <= 76 {
		return 64, swarFloatRelation(kind - 71), true
	}
	return 0, 0, false
}
