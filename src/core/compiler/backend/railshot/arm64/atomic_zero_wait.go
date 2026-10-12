//go:build arm64

package arm64

import railshared "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

var atomicZeroWaitEnabled = true

func (f *fn) atomicZeroWait(d railshared.Atomic) error {
	f.materializePendingLoads()
	f.invalidateStoreForward()
	f.dropValue() // proven literal zero timeout
	expected, owned := f.materializeRead(f.popValue())
	f.pinned = f.pinned.add(expected)
	// Match the helper's alignment-before-bounds trap order. Only low address
	// bits matter here; the ordinary address lowerer still checks full addition.
	address, _ := f.materializeRead(f.s.back())
	align := f.allocReg(maskOf(address, expected))
	f.a.MovReg32(align, address)
	f.addDisp(align, align, int32(uint32(d.Offset)), false)
	if !f.a.TstImm64(align, uint64(d.Size-1)) {
		panic("atomic wait alignment mask")
	}
	f.trapIf(condNE, trapAtomicUnaligned)
	f.release(align)
	addr := f.atomicAddr(d.MemoryIndex, d.Offset, int(d.Size))
	out := f.allocReg(maskOf(addr, expected))
	f.a.Ldar(out, addr, int(d.Size))
	f.cmpRR(out, expected, d.Size == 8)
	f.a.Cset32(out, condE)
	f.a.AddImm32(out, out, 1) // mismatch 1; equal with zero timeout 2
	f.release(addr)
	f.pinned = f.pinned.remove(expected)
	if owned {
		f.release(expected)
	}
	f.pushReg(out, mtI32)
	f.stats.peep("atomic-wait-zero-timeout")
	return nil
}
