//go:build linux && !tinygo

package riscv64

import (
	"slices"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// beginBranchTable clamps the unsigned i32 selector to the default entry,
// loads its label from inline byte data, and jumps over that data. The caller
// appends exactly count+1 bytes, pads to instruction alignment, and patches
// the returned jump to the dispatch code. At most 129 active control frames
// keep every validated label representable in a byte.
func (f *integerEmitter) beginBranchTable(count uint32) (int, error) {
	f.Constant(6, int64(count), false)
	inRange := f.a.BranchLong(enc.LTU, enc.X5, enc.X6)
	f.a.Addi(enc.X5, enc.X6, 0)
	if err := f.Patch(inRange, f.Position()); err != nil {
		return 0, err
	}
	// The table starts 20 bytes after AUIPC: three instructions followed by
	// the eight-byte long jump. Image relocation preserves this displacement.
	f.a.Auipc(enc.X6, 0)
	f.a.Add(enc.X6, enc.X6, enc.X5)
	f.a.Lbu(enc.X5, enc.X6, 20)
	skip := f.Jump()
	// Reserve data and a small dispatch tail together to avoid growing the
	// code buffer repeatedly while labels are streamed into it.
	f.a.B = slices.Grow(f.a.B, int(count)+1+512)
	return skip, f.a.Err()
}
