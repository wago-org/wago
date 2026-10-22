//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Shift consumes pair X6/X7 and raw I32 count X5, returning X6/X7. Direction
// is 0=left, 1=signed right, 2=unsigned right. X10/X11 accumulate isolated
// narrow lanes; X28/X29 and both reserved scratches are temporary.
func (f *integerEmitter) swarShift(width, direction uint8) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	if (width != 8 && width != 16 && width != 32 && width != 64) || direction > 2 {
		f.err = fmt.Errorf("rv64: invalid SWAR shift width/direction")
		return
	}
	// The mask also discards every raw incoming upper carrier bit.
	f.a.Addi(enc.X28, enc.Zero, int32(width-1))
	f.a.And(enc.X28, enc.X5, enc.X28)
	shiftLane := func(dst, src enc.Reg) {
		switch direction {
		case 0:
			f.a.Sll(dst, src, enc.X28)
		case 1:
			f.a.Sra(dst, src, enc.X28)
		case 2:
			f.a.Srl(dst, src, enc.X28)
		}
	}
	if width == 64 {
		shiftLane(enc.Scratch0, enc.X6)
		shiftLane(enc.Scratch1, enc.X7)
		f.a.Addi(enc.X6, enc.Scratch0, 0)
		f.a.Addi(enc.X7, enc.Scratch1, 0)
		return
	}
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	for half, out := range []enc.Reg{enc.X10, enc.X11} {
		src := enc.X6
		if half != 0 {
			src = enc.X7
		}
		for lane := uint8(0); lane < 64/width; lane++ {
			position := lane * width
			f.a.Slli(enc.X29, src, 64-width-position)
			if direction == 1 {
				f.a.Srai(enc.X29, enc.X29, 64-width)
			} else {
				f.a.Srli(enc.X29, enc.X29, 64-width)
			}
			shiftLane(enc.X29, enc.X29)
			if direction != 2 {
				// Discard left-shift overflow and signed extension before packing.
				f.a.Slli(enc.X29, enc.X29, 64-width)
				f.a.Srli(enc.X29, enc.X29, 64-width)
			}
			if position != 0 {
				f.a.Slli(enc.X29, enc.X29, position)
			}
			f.a.Or(out, out, enc.X29)
		}
	}
	f.a.Addi(enc.X6, enc.X10, 0)
	f.a.Addi(enc.X7, enc.X11, 0)
}

func swarShiftOpcode(kind uint32) (width, direction uint8, ok bool) {
	switch kind {
	case 107, 108, 109:
		return 8, uint8(kind - 107), true
	case 139, 140, 141:
		return 16, uint8(kind - 139), true
	case 171, 172, 173:
		return 32, uint8(kind - 171), true
	case 203, 204, 205:
		return 64, uint8(kind - 203), true
	default:
		return 0, 0, false
	}
}
