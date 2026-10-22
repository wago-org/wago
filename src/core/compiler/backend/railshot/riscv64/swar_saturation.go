//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarSaturation uint8

const (
	swarAddSatS swarSaturation = iota
	swarAddSatU
	swarSubSatS
	swarSubSatU
	swarAvgrU
)

// The fixed input pairs are X5/X6 and X7/X9. X28/X29 hold widened lanes,
// X12/X13 hold clamp limits, and X10/X11 retain both complete output halves.
// Widths are at most16, so all sums/differences fit signed64 without overflow.
func (f *integerEmitter) swarSaturate(width uint8, operation swarSaturation) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if (width != 8 && width != 16) || operation > swarAvgrU {
		f.err = fmt.Errorf("rv64: invalid SWAR saturation width/operation")
		return
	}
	signed := operation == swarAddSatS || operation == swarSubSatS
	low, high := int64(0), int64(1<<width)-1
	if signed {
		low, high = -int64(1<<(width-1)), int64(1<<(width-1))-1
	}
	// Clamp comparisons consume signed64 values, including negative limits.
	if operation != swarAvgrU {
		f.Constant(12, low, true)
		f.Constant(13, high, true)
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
			f.a.Slli(enc.X29, b, 64-width-position)
			if signed {
				f.a.Srai(enc.X28, enc.X28, 64-width)
				f.a.Srai(enc.X29, enc.X29, 64-width)
			} else {
				f.a.Srli(enc.X28, enc.X28, 64-width)
				f.a.Srli(enc.X29, enc.X29, 64-width)
			}
			if operation == swarSubSatS || operation == swarSubSatU {
				f.a.Sub(enc.X28, enc.X28, enc.X29)
			} else {
				f.a.Add(enc.X28, enc.X28, enc.X29)
			}
			if operation == swarAvgrU {
				f.a.Addi(enc.X28, enc.X28, 1)
				f.a.Srli(enc.X28, enc.X28, 1)
			} else {
				// Select low when value<low, then high when high<value.
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
			}
			f.a.Slli(enc.X28, enc.X28, 64-width)
			f.a.Srli(enc.X28, enc.X28, 64-width)
			if position != 0 {
				f.a.Slli(enc.X28, enc.X28, position)
			}
			f.a.Or(out, out, enc.X28)
		}
	}
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}

func swarSaturationOpcode(kind uint32) (width uint8, operation swarSaturation, ok bool) {
	switch kind {
	case 111:
		return 8, swarAddSatS, true
	case 112:
		return 8, swarAddSatU, true
	case 114:
		return 8, swarSubSatS, true
	case 115:
		return 8, swarSubSatU, true
	case 123:
		return 8, swarAvgrU, true
	case 143:
		return 16, swarAddSatS, true
	case 144:
		return 16, swarAddSatU, true
	case 146:
		return 16, swarSubSatS, true
	case 147:
		return 16, swarSubSatU, true
	case 155:
		return 16, swarAvgrU, true
	default:
		return 0, 0, false
	}
}
