//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func (f *integerEmitter) swarLaneCheck(width, lane uint8) bool {
	if f.err != nil || f.a.Err() != nil {
		return false
	}
	if (width != 8 && width != 16 && width != 32) || lane >= 128/width {
		f.err = fmt.Errorf("rv64: invalid scalar SWAR lane width/index")
		return false
	}
	return true
}

// Splat consumes the low width bits of raw I32 X5 and returns a pair in X6/X7.
// X29 is temporary. Doubling the already replicated chunk cannot carry into
// another lane; no optional vector or bit-manipulation extension is used.
func (f *integerEmitter) swarSplat(width uint8) {
	if !f.swarLaneCheck(width, 0) {
		return
	}
	f.a.Slli(enc.X6, enc.X5, 64-width)
	f.a.Srli(enc.X6, enc.X6, 64-width)
	for shift := width; shift < 64; shift *= 2 {
		f.a.Slli(enc.X29, enc.X6, shift)
		f.a.Or(enc.X6, enc.X6, enc.X29)
	}
	f.a.Addi(enc.X7, enc.X6, 0)
}

// Extract reads pair X6/X7 and writes a normalized I32 to X5. Signed narrow
// lanes extend into the low32 bits, then clear the upper32 carrier bits.
func (f *integerEmitter) swarExtract(width, lane uint8, signed bool) {
	if !f.swarLaneCheck(width, lane) {
		return
	}
	src := enc.X6
	if lane >= 64/width {
		src = enc.X7
	}
	shift := (lane % (64 / width)) * width
	f.a.Slli(enc.X5, src, 64-width-shift)
	if signed {
		f.a.Srai(enc.X5, enc.X5, 64-width)
		f.NormalizeI32(5)
	} else {
		f.a.Srli(enc.X5, enc.X5, 64-width)
	}
}

// Replace reads pair X6/X7 and the low width bits of raw I32 X5. Only the
// selected lane changes; X28/X29 are temporary, including high-half lanes.
func (f *integerEmitter) swarReplace(width, lane uint8) {
	if !f.swarLaneCheck(width, lane) {
		return
	}
	dst := enc.X6
	if lane >= 64/width {
		dst = enc.X7
	}
	shift := (lane % (64 / width)) * width
	mask := (uint64(1)<<width - 1) << shift
	f.a.Constant64(enc.X28, int64(^mask))
	f.a.And(dst, dst, enc.X28)
	f.a.Slli(enc.X29, enc.X5, 64-width)
	f.a.Srli(enc.X29, enc.X29, 64-width)
	if shift != 0 {
		f.a.Slli(enc.X29, enc.X29, shift)
	}
	f.a.Or(dst, dst, enc.X29)
}
