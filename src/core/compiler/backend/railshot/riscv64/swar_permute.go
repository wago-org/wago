//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Shuffle reads X5/X6 and X7/X9, and returns X5/X6. Immediate indices select
// from the concatenated32 bytes. X10/X11 accumulate output, X28 is temporary.
func (f *integerEmitter) swarShuffle(indices [16]byte) {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	for _, index := range indices {
		if index >= 32 {
			f.err = fmt.Errorf("rv64: invalid SWAR shuffle byte index")
			return
		}
	}
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	sources := [4]enc.Reg{enc.X5, enc.X6, enc.X7, enc.X9}
	for lane, index := range indices {
		out := enc.X10
		if lane >= 8 {
			out = enc.X11
		}
		f.a.Slli(enc.X28, sources[index/8], 56-(index%8)*8)
		f.a.Srli(enc.X28, enc.X28, 56)
		if position := uint8(lane%8) * 8; position != 0 {
			f.a.Slli(enc.X28, enc.X28, position)
		}
		f.a.Or(out, out, enc.X28)
	}
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}

// Swizzle reads bytes X5/X6 and runtime indices X7/X9, returning X5/X6.
// Every index >=16 yields a zero byte. X10/X11 collect outputs; X28/X29,
// Scratch0 (limits) and Scratch1 (long branch veneer) are temporary.
func (f *integerEmitter) swarSwizzle() {
	if f.err != nil || f.a.Err() != nil {
		return
	}
	f.a.Addi(enc.X10, enc.Zero, 0)
	f.a.Addi(enc.X11, enc.Zero, 0)
	for lane := uint8(0); lane < 16; lane++ {
		indices, out := enc.X7, enc.X10
		if lane >= 8 {
			indices, out = enc.X9, enc.X11
		}
		position := (lane % 8) * 8
		f.a.Slli(enc.X28, indices, 56-position)
		f.a.Srli(enc.X28, enc.X28, 56)
		f.a.Addi(enc.X29, enc.Zero, 0)
		f.a.Addi(enc.Scratch0, enc.Zero, 16)
		invalid := f.a.BranchLong(enc.GEU, enc.X28, enc.Scratch0)
		f.a.Addi(enc.Scratch0, enc.Zero, 8)
		low := f.a.BranchLong(enc.LTU, enc.X28, enc.Scratch0)
		f.a.Addi(enc.X29, enc.X6, 0)
		f.a.Addi(enc.X28, enc.X28, -8)
		ready := f.Jump()
		if err := f.Patch(low, f.Position()); err != nil {
			f.err = err
			return
		}
		f.a.Addi(enc.X29, enc.X5, 0)
		if err := f.Patch(ready, f.Position()); err != nil {
			f.err = err
			return
		}
		f.a.Slli(enc.X28, enc.X28, 3)
		f.a.Srl(enc.X29, enc.X29, enc.X28)
		f.a.Slli(enc.X29, enc.X29, 56)
		f.a.Srli(enc.X29, enc.X29, 56)
		if err := f.Patch(invalid, f.Position()); err != nil {
			f.err = err
			return
		}
		if position != 0 {
			f.a.Slli(enc.X29, enc.X29, position)
		}
		f.a.Or(out, out, enc.X29)
	}
	f.a.Addi(enc.X5, enc.X10, 0)
	f.a.Addi(enc.X6, enc.X11, 0)
}
