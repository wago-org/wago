//go:build arm64

package arm64

import (
	railshared "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Read canonical operands after caller homes are saved, before marshaling any
// host-call arguments. Invalid addresses use the helper's existing traps.
// The returned branch joins the ordinary result/local reconstruction.
func (f *fn) emitAtomicWaitFastPath(helper uint32, slot int, offset uint32) int {
	size := 4
	if helper == railshared.AtomicHelperWait64 {
		size = 8
	}
	f.ld32(X9, SP, f.spillOff(slot))
	if offset != 0 {
		f.a.MovImm64(X16, uint64(offset))
		f.a.Add64(X9, X9, X16)
	}
	f.a.TstImm64(X9, uint64(size-1))
	badAlignment := f.a.Bcond(condNE)
	f.ld64(X10, linMemReg, -int32(offMemoryDirPtr))
	f.ld64(X10, X10, abi.MemoryDirBaseOffset)
	f.loadSharedMemoryBytes(X16, X10)
	f.a.AddImm64(X11, X9, uint32(size))
	f.cmpRR(X11, X16, true)
	outOfBounds := f.a.Bcond(condA)
	var slow int
	if helper == railshared.AtomicHelperNotify {
		f.a.SubImm64(X16, X10, abi.SharedNotifyPendingOffset)
		// Order prior guest stores before observing wait's SC announcement.
		// Acquire alone cannot provide the required store/load ordering.
		f.a.DmbIsh()
		f.a.Ldar(X9, X16, 4)
		slow = f.a.Cbnz32(X9)
		f.a.MovImm64(X9, 0)
	} else {
		f.a.Add64(X10, X10, X9)
		f.a.Ldar(X9, X10, size)
		f.ld64(X10, SP, f.spillOff(slot+1))
		f.cmpRR(X9, X10, size == 8)
		// Repeat matching comparisons under the helper's queue mutex, including
		// timeout zero, so the comparison and queue operation stay synchronized.
		slow = f.a.Bcond(condE)
		f.a.MovImm64(X9, 1)
	}
	f.ld64(X11, linMemReg, -int32(offCustomCtx))
	f.st64(X11, hcResults, X9)
	done := f.a.Branch()
	f.patchBranch19(slow, f.a.Len())
	f.patchBranch19(badAlignment, f.a.Len())
	f.patchBranch19(outOfBounds, f.a.Len())
	return done
}
