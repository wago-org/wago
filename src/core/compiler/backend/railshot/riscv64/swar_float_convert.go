//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarFloatConversion struct {
	inputWidth, outputWidth, lanes uint8
	scalarOp                       byte
	trunc, unsigned                bool
}

func swarFloatConversionOpcode(kind uint32) (swarFloatConversion, bool) {
	switch kind {
	case 94:
		return swarFloatConversion{64, 32, 2, 0xb6, false, false}, true
	case 95:
		return swarFloatConversion{32, 64, 2, 0xbb, false, false}, true
	case 248, 249:
		return swarFloatConversion{32, 32, 4, 0, true, kind == 249}, true
	case 250:
		return swarFloatConversion{32, 32, 4, 0xb2, false, false}, true
	case 251:
		return swarFloatConversion{32, 32, 4, 0xb3, false, false}, true
	case 252, 253:
		return swarFloatConversion{64, 32, 2, 0, true, kind == 253}, true
	case 254:
		return swarFloatConversion{32, 64, 2, 0xb7, false, false}, true
	case 255:
		return swarFloatConversion{32, 64, 2, 0xb8, false, false}, true
	default:
		return swarFloatConversion{}, false
	}
}

// Save the entire source pair while qualified scalar conversion routines
// clobber GP5/6/7 and FP0. Zeroed output halves provide the required upper zero
// lanes for demote and trunc_f64_zero, and low conversions never read high lanes.
func (f *integerEmitter) swarFloatConvert(kind uint32) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	conversion, ok := swarFloatConversionOpcode(kind)
	if !ok {
		f.err = fmt.Errorf("rv64: invalid SWAR floating conversion opcode")
		return
	}
	f.a.Addi(enc.X14, enc.X5, 0)
	f.a.Addi(enc.X15, enc.X6, 0)
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	for lane := uint8(0); lane < conversion.lanes; lane++ {
		inputPosition := lane * conversion.inputWidth
		src := enc.X14
		if inputPosition >= 64 {
			src = enc.X15
		}
		f.a.Slli(enc.X5, src, 64-conversion.inputWidth-inputPosition%64)
		f.a.Srli(enc.X5, enc.X5, 64-conversion.inputWidth)
		if conversion.trunc {
			f.floatTrunc(conversion.inputWidth == 64, false, conversion.unsigned, true)
		} else {
			f.floatConvert(conversion.scalarOp)
		}
		outputPosition := lane * conversion.outputWidth
		out := enc.X10
		if outputPosition >= 64 {
			out = enc.X11
		}
		if outputPosition%64 != 0 {
			f.a.Slli(enc.X5, enc.X5, outputPosition%64)
		}
		f.a.Or(out, out, enc.X5)
	}
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}
