//go:build linux && !tinygo

package riscv64

import enc "github.com/wago-org/wago/src/core/encoder/riscv64"

const trapNullReference = 13 // private tag; production TrapNullReference is 16

// A reference is a full-width opaque carrier. The check neither dereferences
// it nor changes its stack home, including the upper half of a SWAR carrier.
func (f *integerEmitter) referenceAsNonNull(offset int32) {
	f.Load(5, offset, true)
	f.trap(trapNullReference, f.a.BranchLong(enc.EQ, enc.X5, enc.Zero))
}
