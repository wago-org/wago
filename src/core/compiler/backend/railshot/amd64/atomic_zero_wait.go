//go:build amd64

package amd64

import railshared "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

var atomicZeroWaitEnabled = true

func (f *fn) atomicZeroWait(d railshared.Atomic) error {
	f.materializePendingLoads()
	f.invalidateStoreForward()
	f.dropValue() // proven literal zero timeout
	expected, owned := f.materializeRead(f.popValue())
	f.pinned = f.pinned.add(expected)
	// Preserve alignment-before-bounds traps, including offsets that cross u32.
	address, _ := f.materializeRead(f.s.back())
	align := f.allocReg(maskOf(address).add(expected))
	f.a.MovRegReg32(align, address)
	f.a.AluRI(0, align, int32(uint32(d.Offset)), false)
	f.a.TestImm(align, uint32(d.Size-1), false)
	f.trapIf(condNE, trapAtomicUnaligned)
	f.release(align)
	base, ea, disp := f.atomicMem(d.MemoryIndex, d.Offset, int(d.Size))
	out := f.allocReg(maskOf(base).add(ea).add(expected))
	f.a.LoadIdx(out, base, ea, disp, int(d.Size), false, d.Size == 8)
	f.a.AluRR(0x39, out, expected, d.Size == 8)
	f.a.SetccReg(condE, out)
	f.a.AluRI(0, out, 1, false)
	f.release(base)
	f.release(ea)
	f.pinned = f.pinned.remove(expected)
	if owned {
		f.release(expected)
	}
	f.pushReg(out, mtI32)
	f.stats.peep("atomic-wait-zero-timeout")
	return nil
}

// A zero notification count still validates the address and orders memory.
func (f *fn) atomicZeroNotify(d railshared.Atomic) error {
	f.materializePendingLoads()
	f.invalidateStoreForward()
	f.dropValue()
	address, _ := f.materializeRead(f.s.back())
	align := f.allocReg(maskOf(address))
	f.a.MovRegReg32(align, address)
	f.a.AluRI(0, align, int32(uint32(d.Offset)), false)
	f.a.TestImm(align, 3, false)
	f.trapIf(condNE, trapAtomicUnaligned)
	f.release(align)
	base, ea, _ := f.atomicMem(d.MemoryIndex, d.Offset, 4)
	f.release(base)
	f.release(ea)
	// Preserve queue-event ordering even though no waiter can wake.
	f.a.Mfence()
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0})
	f.stats.peep("atomic-notify-empty")
	return nil
}
