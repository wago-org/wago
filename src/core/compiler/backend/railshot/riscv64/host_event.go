//go:build linux && !tinygo

package riscv64

import (
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// HostIndirectThunk records one capability-free i32 event in the owning
// instance's bounded log. It uses the same serialized wrapper ABI as imported
// guest calls. Public binding checks admit only (i32)->() event callbacks.
func HostIndirectThunk(index uint32) []byte {
	var a enc.Asm
	a.Ld(enc.X28, enc.X11, -40)
	missing := a.BranchLong(enc.EQ, enc.X28, enc.Zero)
	a.Lwu(enc.X29, enc.X28, 0)
	a.Constant32(enc.X30, coreruntime.HostCallLogEntries)
	full := a.BranchLong(enc.GEU, enc.X29, enc.X30)
	a.Slli(enc.X30, enc.X29, 3)
	a.Add(enc.X30, enc.X28, enc.X30)
	a.Constant32(enc.X31, index)
	a.Sw(enc.X31, enc.X30, 8)
	a.Lwu(enc.X31, enc.X10, 0)
	a.Sw(enc.X31, enc.X30, 12)
	a.Addi(enc.X29, enc.X29, 1)
	a.Sw(enc.X29, enc.X28, 0)
	a.Ret()
	overflow := a.Len()
	a.Constant32(enc.X5, uint32(coreruntime.TrapHostEventOverflow))
	toTrap := a.JumpLong(false)
	absent := a.Len()
	a.Constant32(enc.X5, abi.TrapCalledFnNotLinked)
	trap := a.Len()
	a.Ld(enc.X12, enc.X11, -abi.TrapCellPtrOffset)
	a.Sw(enc.X5, enc.X12, 0)
	a.Ld(enc.SP, enc.X11, -abi.RV64TrapReentrySPOffset)
	a.Ld(enc.X31, enc.X11, -abi.RV64TrapContinuationOffset)
	a.Jalr(enc.Zero, enc.X31, 0)
	if a.PatchLong(missing, int64(absent)) != nil || a.PatchLong(full, int64(overflow)) != nil || a.PatchLong(toTrap, int64(trap)) != nil || a.Err() != nil {
		return nil
	}
	return a.B
}
