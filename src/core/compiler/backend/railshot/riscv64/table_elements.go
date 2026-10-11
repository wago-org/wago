//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Original module indices address16-byte descriptors; active/declarative slots
// start dropped. Materialize the address for every index, including255.
func (f *integerEmitter) privateElementAddress(index uint32) {
	f.a.Ld(enc.X28, enc.X25, -abi.PassiveElemPtrOffset)
	f.trap(trapTableBounds, f.a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	f.a.Constant64(enc.X29, int64(index)*16)
	f.a.Add(enc.X28, enc.X28, enc.X29)
}

func (f *integerEmitter) privateElementDrop(index uint32) {
	f.privateElementAddress(index)
	f.a.Sw(enc.Zero, enc.X28, 8)
}

func (f *integerEmitter) privateTableInit(index uint32) {
	f.privateTableInitAt(index, 0, false)
}

func (f *integerEmitter) privateTableInitAt(elementIndex, tableIndex uint32, compactReference bool) {
	// X5=count,X6=destination index,X7=source element index. Validate both
	// complete ranges before writing or dereferencing the segment's payload.
	f.privateTableRangeAt(false, tableIndex, compactReference)
	f.privateElementAddress(elementIndex)
	f.a.Lwu(enc.X29, enc.X28, 8)
	f.a.Add(enc.X30, enc.X7, enc.X5)
	f.trap(trapTableBounds, f.a.BranchLong(enc.LTU, enc.X29, enc.X30))
	done := f.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
	f.a.Ld(enc.X28, enc.X28, 0)
	f.trap(trapTableBounds, f.a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	shift := uint8(5)
	step := int32(32)
	if compactReference {
		shift, step = 3, 8
	}
	f.a.Slli(enc.X7, enc.X7, shift)
	f.a.Add(enc.X7, enc.X7, enc.X28)
	f.privateCopyEntries(step, compactReference)
	if err := f.Patch(done, f.Position()); err != nil {
		f.err = err
	}
}
