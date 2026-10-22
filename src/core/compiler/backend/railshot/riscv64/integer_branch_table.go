//go:build linux && !tinygo

package riscv64

import (
	"slices"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// beginBranchTable clamps the unsigned i32 selector to the default entry,
// loads its label from inline byte or halfword data, and jumps over that data.
// The caller appends exactly count+1 labels, pads to instruction alignment,
// and patches the returned jump to the dispatch code.
func (f *integerEmitter) beginBranchTable(count uint32, wideLabels bool) (int, error) {
	f.Constant(6, int64(count), false)
	inRange := f.a.BranchLong(enc.LTU, enc.X5, enc.X6)
	f.a.Addi(enc.X5, enc.X6, 0)
	if err := f.Patch(inRange, f.Position()); err != nil {
		return 0, err
	}
	if wideLabels {
		f.a.Slli(enc.X5, enc.X5, 1)
	}
	// The table starts 20 bytes after AUIPC: three instructions followed by
	// the eight-byte long jump. Image relocation preserves this displacement.
	f.a.Auipc(enc.X6, 0)
	f.a.Add(enc.X6, enc.X6, enc.X5)
	if wideLabels {
		f.a.Lhu(enc.X5, enc.X6, 20)
	} else {
		f.a.Lbu(enc.X5, enc.X6, 20)
	}
	skip := f.Jump()
	// Reserve data and a small dispatch tail together to avoid growing the
	// code buffer repeatedly while labels are streamed into it.
	labelBytes := 1
	if wideLabels {
		labelBytes = 2
	}
	f.a.B = slices.Grow(f.a.B, (int(count)+1)*labelBytes+512)
	return skip, f.a.Err()
}
