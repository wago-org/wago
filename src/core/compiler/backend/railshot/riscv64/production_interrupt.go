//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const trapCooperativeInterrupt = 6 // private emission tag; production code is 12

// Go atomically publishes TrapInterrupted in the invocation cell. Aligned LWU
// plus FENCE R,RW forms an acquire read on the baseline weakly ordered RV64 ISA.
// Only X28/X29 are clobbered; entry transport and bulk cursors stay intact.
func (f *target) productionInterruptLoad() {
	f.a.Ld(enc.X28, enc.X25, -abi.TrapCellPtrOffset)
	f.a.Lwu(enc.X29, enc.X28, 0)
	f.a.Fence(enc.FenceRead, enc.FenceRead|enc.FenceWrite)
}

func (f *target) emitProductionEntryPoll() {
	if !f.mode.production || !f.mode.interruptible {
		return
	}
	f.productionInterruptLoad()
	clear := f.a.BranchLong(enc.EQ, enc.X29, enc.Zero)
	f.emitProductionTrap(abi.TrapInterrupted, f.mode.entryPC)
	if err := f.Patch(clear, f.Position()); err != nil {
		f.err = err
	}
}

func (f *integerEmitter) emitProductionInterruptPoll() {
	if !f.mode.production || !f.mode.interruptible {
		return
	}
	f.productionInterruptLoad()
	f.trap(trapCooperativeInterrupt, f.a.BranchLong(enc.NE, enc.X29, enc.Zero))
}
