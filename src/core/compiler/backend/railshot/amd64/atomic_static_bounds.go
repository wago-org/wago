//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

var atomicStaticBoundsEnabled = true

func (f *fn) staticAtomicMem(memoryIndex uint32, offset uint64, size int) (base, ea Reg, ok bool) {
	if memoryIndex != 0 || !f.opt(optAtomicStaticBounds) {
		return regNone, regNone, false
	}
	e := f.s.back()
	mt, valid := f.memoryType(0)
	if !valid || e == nil || !e.isValue() || e.st.kind != stConst {
		return regNone, regNone, false
	}
	address, valid := shared.ProvenAtomicAddress(mt, uint32(e.st.cval), offset, size)
	if !valid {
		return regNone, regNone, false
	}
	f.materializePendingTraps()
	f.dropValue()
	base = f.allocReg(0)
	if !f.threadedMemory0 {
		f.a.MovReg64(base, RBX)
	} else if cached := f.cachedSharedMemoryBase(); cached != regNone {
		f.a.MovReg64(base, cached)
	} else {
		f.a.Load64(base, RBX, -offMemoryDirPtr)
		f.a.Load64(base, base, 0)
	}
	ea = f.allocReg(maskOf(base))
	f.a.MovImm64(ea, address)
	f.stats.peep("atomic-static-bounds")
	return base, ea, true
}
