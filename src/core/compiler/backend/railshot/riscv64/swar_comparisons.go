//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarComparison uint8

const (
	swarEQ swarComparison = iota
	swarNE
	swarLTS
	swarLTU
	swarGTS
	swarGTU
	swarLES
	swarLEU
	swarGES
	swarGEU
)

// Compare reads pairs X5/X6 and X7/X9, returning full-lane masks in X5/X6.
// X28/X29 hold isolated lanes; X10/X11 accumulate both outputs before commit,
// and Scratch0 holds the normalized Boolean and then its all-ones mask.
func (f *integerEmitter) swarCompare(width uint8, relation swarComparison) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if (width != 8 && width != 16 && width != 32 && width != 64) || relation > swarGEU ||
		(width == 64 && (relation == swarLTU || relation == swarGTU || relation == swarLEU || relation == swarGEU)) {
		f.err = fmt.Errorf("rv64: invalid SWAR comparison width/relation")
		return
	}
	signed := relation == swarLTS || relation == swarGTS || relation == swarLES || relation == swarGES
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
			switch relation {
			case swarEQ, swarNE:
				f.a.Xor(enc.Scratch0, enc.X28, enc.X29)
				if relation == swarEQ {
					f.a.Sltiu(enc.Scratch0, enc.Scratch0, 1)
				} else {
					f.a.Sltu(enc.Scratch0, enc.Zero, enc.Scratch0)
				}
			case swarLTS, swarGES:
				f.a.Slt(enc.Scratch0, enc.X28, enc.X29)
			case swarLTU, swarGEU:
				f.a.Sltu(enc.Scratch0, enc.X28, enc.X29)
			case swarGTS, swarLES:
				f.a.Slt(enc.Scratch0, enc.X29, enc.X28)
			case swarGTU, swarLEU:
				f.a.Sltu(enc.Scratch0, enc.X29, enc.X28)
			}
			if relation == swarLES || relation == swarLEU || relation == swarGES || relation == swarGEU {
				f.a.Xori(enc.Scratch0, enc.Scratch0, 1)
			}
			f.a.Sub(enc.Scratch0, enc.Zero, enc.Scratch0)
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

func swarComparisonOpcode(kind uint32) (width uint8, relation swarComparison, ok bool) {
	switch {
	case kind >= 35 && kind <= 44:
		return 8, swarComparison(kind - 35), true
	case kind >= 45 && kind <= 54:
		return 16, swarComparison(kind - 45), true
	case kind >= 55 && kind <= 64:
		return 32, swarComparison(kind - 55), true
	}
	switch kind {
	case 214:
		return 64, swarEQ, true
	case 215:
		return 64, swarNE, true
	case 216:
		return 64, swarLTS, true
	case 217:
		return 64, swarGTS, true
	case 218:
		return 64, swarLES, true
	case 219:
		return 64, swarGES, true
	default:
		return 0, 0, false
	}
}
