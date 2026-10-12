//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

var atomicStaticBoundsEnabled = true

func (f *fn) staticAtomicAddr(offset uint64, size int) (Reg, bool) {
	if !f.opt(optAtomicStaticBounds) {
		return regNone, false
	}
	e := f.s.back()
	mt, ok := f.memoryType(0)
	if !ok || e == nil || e.elemKind() != ekValue || e.st.kind != stConst {
		return regNone, false
	}
	ea, ok := shared.ProvenAtomicAddress(mt, uint32(e.st.cval), offset, size)
	if !ok {
		return regNone, false
	}
	f.materializePendingTraps()
	f.dropValue()
	addr := f.allocReg(0)
	f.ld64(addr, linMemReg, -int32(offMemoryDirPtr))
	f.ld64(addr, addr, 0) // memory zero backing base
	if ea <= 0x7fffffff {
		f.addDisp(addr, addr, int32(ea), true)
	} else {
		t := f.allocReg(maskOf(addr))
		f.a.MovImm64(t, ea)
		f.a.Add64(addr, addr, t)
		f.release(t)
	}
	f.stats.peep("atomic-static-bounds")
	return addr, true
}
