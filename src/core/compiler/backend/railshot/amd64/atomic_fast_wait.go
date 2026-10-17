//go:build amd64

package amd64

import (
	railshared "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// emitAtomicWaitFastPath reads canonical operand slots before marshaling any
// host-call arguments. Caller-saved registers are available after local spills.
// The returned branch skips the Go transition and joins its result reload.
// Invalid arguments use Go's existing traps instead of duplicate native stubs.
func (f *fn) emitAtomicWaitFastPath(helper uint32, slot int, offset uint32) int {
	f.a.Load32(RAX, RSP, f.spillOff(slot))
	size := int32(4)
	if helper != railshared.AtomicHelperNotify {
		if helper == railshared.AtomicHelperWait64 {
			size = 8
		}
	}
	if offset != 0 {
		f.a.MovImm32(RDI, int32(offset))
		f.a.Add64(RAX, RDI)
	}
	f.a.TestImm(RAX, uint32(size-1), false)
	badAlignment := f.a.JccPlaceholder(condNE)
	f.a.Load64(RSI, RBX, -offMemoryDirPtr)
	f.a.Load64(RSI, RSI, abi.MemoryDirBaseOffset)
	f.a.Load64(RDI, RSI, -abi.ActualLinMemByteSize64Offset)
	f.a.AluRI(0, RAX, size, true)
	f.a.Cmp64(RAX, RDI)
	outOfBounds := f.a.JccPlaceholder(condA)
	if helper == railshared.AtomicHelperNotify {
		// Pair with wait's announcement before its value load. Prior guest stores
		// must be visible before concluding that nobody can need this wake.
		// A locked no-op on this activation's stack avoids writing the shared
		// queue-state cache line while providing the required store/load fence.
		f.a.MovImm32(RAX, 0)
		f.a.LockAluIdx(0x09, RSP, RAX, RAX, 0, 4)
		f.a.Load32(RDI, RSI, -abi.SharedNotifyPendingOffset)
		f.a.TestSelf(RDI, false)
		slow := f.a.JccPlaceholder(condNE)
		f.a.MovImm32(RAX, 0)
		f.a.Load64(R8, RBX, -offCustomCtx)
		f.a.Store64(R8, hcResults, RAX)
		done := f.a.JmpPlaceholder()
		f.a.PatchRel32(slow, f.a.Len())
		f.a.PatchRel32(badAlignment, f.a.Len())
		f.a.PatchRel32(outOfBounds, f.a.Len())
		return done
	}
	f.a.Load64(RDI, RSP, f.spillOff(slot+1))
	f.a.LoadIdx(RAX, RSI, RAX, -size, int(size), false, size == 8)
	if size == 8 {
		f.a.Cmp64(RAX, RDI)
	} else {
		f.a.Cmp32(RAX, RDI)
	}
	// A matching wait must synchronize its comparison with the queue operation,
	// even for timeout zero. Let the helper repeat it under the queue mutex.
	slow := f.a.JccPlaceholder(condE)
	f.a.MovImm32(RAX, 1)
	f.a.Load64(R8, RBX, -offCustomCtx)
	f.a.Store64(R8, hcResults, RAX)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(slow, f.a.Len())
	f.a.PatchRel32(badAlignment, f.a.Len())
	f.a.PatchRel32(outOfBounds, f.a.Len())
	return done
}
