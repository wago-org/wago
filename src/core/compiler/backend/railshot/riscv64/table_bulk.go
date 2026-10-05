//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Mutation keeps count in X5, destination in X6, and source/reference in X7.
// Polls clobber only X28/X29, and address/long-transfer helpers use X31.
func (f *integerEmitter) privateTableRange(copySource bool) {
	f.a.Ld(enc.X28, enc.X25, -80)
	f.trap(trapTableBounds, f.a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	f.a.Lwu(enc.X29, enc.X28, 0)
	f.a.Add(enc.X30, enc.X6, enc.X5)
	f.trap(trapTableBounds, f.a.BranchLong(enc.LTU, enc.X29, enc.X30))
	if copySource {
		f.a.Add(enc.X30, enc.X7, enc.X5)
		f.trap(trapTableBounds, f.a.BranchLong(enc.LTU, enc.X29, enc.X30))
	}
	// Normalized table32 operands add in64 bits: even two UINT32_MAX values
	// cannot wrap. Exact-end zero-length ranges remain valid.
	f.a.Slli(enc.X6, enc.X6, 5)
	f.a.Add(enc.X6, enc.X6, enc.X28)
	f.a.Addi(enc.X6, enc.X6, 8)
	if copySource {
		f.a.Slli(enc.X7, enc.X7, 5)
		f.a.Add(enc.X7, enc.X7, enc.X28)
		f.a.Addi(enc.X7, enc.X7, 8)
	}
}

func (f *integerEmitter) privateReferenceGuard() {
	null := f.a.BranchLong(enc.EQ, enc.X7, enc.Zero)
	f.a.Ld(enc.X28, enc.X7, 16)
	f.a.Constant64(enc.X29, int64(abi.FuncRefLocalWrapperHomeTag))
	f.a.Or(enc.X29, enc.X29, enc.X25)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X28, enc.X29))
	f.a.Ld(enc.X28, enc.X7, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	f.a.Ld(enc.X29, enc.X25, -88)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.EQ, enc.X29, enc.Zero))
	f.a.Ld(enc.X29, enc.X29, 32)
	f.trap(trapIndirectBounds, f.a.BranchLong(enc.NE, enc.X28, enc.X29))
	if err := f.Patch(null, f.Position()); err != nil {
		f.err = err
	}
}

func (f *integerEmitter) privateTableFill() {
	f.privateTableRange(false)
	f.privateReferenceGuard()
	f.privateFillEntries()
}

// X30 may retain grow's old length. No polling edge splits the four stores.
func (f *integerEmitter) privateFillEntries() {
	done := f.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
	loop := f.Position()
	f.emitProductionInterruptPoll()
	null := f.a.BranchLong(enc.EQ, enc.X7, enc.Zero)
	for offset := int32(0); offset < 32; offset += 8 {
		f.a.Ld(enc.X28, enc.X7, offset)
		f.a.Sd(enc.X28, enc.X6, offset)
	}
	copied := f.Jump()
	if err := f.Patch(null, f.Position()); err != nil {
		f.err = err
	}
	for offset := int32(0); offset < 32; offset += 8 {
		f.a.Sd(enc.Zero, enc.X6, offset)
	}
	if err := f.Patch(copied, f.Position()); err != nil {
		f.err = err
	}
	f.a.Addi(enc.X6, enc.X6, 32)
	f.a.Addi(enc.X5, enc.X5, -1)
	if err := f.Patch(f.a.BranchLong(enc.NE, enc.X5, enc.Zero), loop); err != nil {
		f.err = err
	}
	if err := f.Patch(done, f.Position()); err != nil {
		f.err = err
	}
}

func (f *integerEmitter) privateTableCopy() {
	f.privateTableRange(true)
	done := f.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
	forward := f.a.BranchLong(enc.GEU, enc.X7, enc.X6)
	// A backwards copy is safe for every dst>src range, overlapping or disjoint.
	f.a.Slli(enc.X28, enc.X5, 5)
	f.a.Add(enc.X6, enc.X6, enc.X28)
	f.a.Add(enc.X7, enc.X7, enc.X28)
	f.a.Addi(enc.X6, enc.X6, -32)
	f.a.Addi(enc.X7, enc.X7, -32)
	f.privateCopyEntries(-32)
	backDone := f.Jump()
	if err := f.Patch(forward, f.Position()); err != nil {
		f.err = err
	}
	f.privateCopyEntries(32)
	if err := f.Patch(backDone, f.Position()); err != nil {
		f.err = err
	}
	if err := f.Patch(done, f.Position()); err != nil {
		f.err = err
	}
}

func (f *integerEmitter) privateCopyEntries(step int32) {
	loop := f.Position()
	f.emitProductionInterruptPoll()
	for offset := int32(0); offset < 32; offset += 8 {
		f.a.Ld(enc.X28, enc.X7, offset)
		f.a.Sd(enc.X28, enc.X6, offset)
	}
	f.a.Addi(enc.X6, enc.X6, step)
	f.a.Addi(enc.X7, enc.X7, step)
	f.a.Addi(enc.X5, enc.X5, -1)
	if err := f.Patch(f.a.BranchLong(enc.NE, enc.X5, enc.Zero), loop); err != nil {
		f.err = err
	}
}

func (f *integerEmitter) privateTableGrow(deltaSlot int) {
	f.a.Ld(enc.X28, enc.X25, -80)
	f.trap(trapTableBounds, f.a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	f.a.Lwu(enc.X30, enc.X28, 0)
	f.a.Lwu(enc.X29, enc.X28, 4)
	f.a.Add(enc.X6, enc.X30, enc.X5)
	failed := f.a.BranchLong(enc.LTU, enc.X29, enc.X6)
	f.privateReferenceGuard()
	f.a.Ld(enc.X6, enc.X25, -80)
	f.a.Slli(enc.X28, enc.X30, 5)
	f.a.Add(enc.X6, enc.X6, enc.X28)
	f.a.Addi(enc.X6, enc.X6, 8)
	f.privateFillEntries()
	// Initialization and all entry polls finish before the new length is visible.
	f.Load(5, f.SpillOffset(deltaSlot), false)
	f.a.Add(enc.X5, enc.X30, enc.X5)
	f.a.Ld(enc.X6, enc.X25, -80)
	f.a.Sw(enc.X5, enc.X6, 0)
	f.a.Addi(enc.X5, enc.X30, 0)
	done := f.Jump()
	if err := f.Patch(failed, f.Position()); err != nil {
		f.err = err
	}
	f.a.Constant32(enc.X5, ^uint32(0))
	if err := f.Patch(done, f.Position()); err != nil {
		f.err = err
	}
}
