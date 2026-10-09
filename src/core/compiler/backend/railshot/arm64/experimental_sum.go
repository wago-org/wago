//go:build arm64

package arm64

// The legality proof is shared with the original sum. ARM64 keeps X16 as its
// reserved address/load scratch; accumulator allocation is target-specific.
func (f *fn) tryExperimentalSumLatch(counter, factor, chains int) bool {
	if f.linearSumLoop == 0 || f.linearSumLoopDepth != uint16(len(f.ctrl)) {
		return false
	}
	addr, acc := int(uint16(f.linearSumLoop)-1), int(uint16(f.linearSumLoop>>16)-1)
	cr, cf, cp := f.pinReg(counter)
	ar, af, ap := f.pinReg(addr)
	sr, sf, sp := f.pinReg(acc)
	if !cp || cf || !ap || af || !sp || sf {
		return false
	}
	var regs [4]Reg
	regs[0] = sr
	for i := 1; i < chains; i++ {
		regs[i] = f.allocReg(0)
		f.pinned = f.pinned.add(regs[i])
		f.a.MovImm64(regs[i], 0)
	}
	f.a.SubsImm32(cr, cr, 1)
	first := f.a.Bcond(condE)
	f.a.CmpImm32(cr, uint32(factor))
	short := f.a.Bcond(condB)
	wrap := -1
	mt, _ := f.m.MemoryType(0)
	if !mt.Limits.HasMax || mt.Limits.Max >= 65536 {
		f.a.MovReg32(X16, cr)
		f.a.LslImm(X16, X16, 3, false)
		f.a.Add64(X16, X16, ar)
		f.a.LsrImm(X16, X16, 32, false)
		wrap = f.a.Cbnz64(X16)
	}
	group := f.a.Len()
	for i := 0; i < factor; i++ {
		f.a.LoadIdx(X16, linMemReg, ar, int32(i*8), 8, false, true)
		f.a.Add64(regs[i%chains], regs[i%chains], X16)
	}
	f.a.AddImm32(ar, ar, uint32(factor*8))
	f.a.SubImm32(cr, cr, uint32(factor))
	f.a.CmpImm32(cr, uint32(factor))
	back := f.a.Bcond(condAE)
	f.patchBranch19(back, group)
	f.patchBranch19(short, f.a.Len())
	if wrap >= 0 {
		f.patchBranch19(wrap, f.a.Len())
	}
	empty := f.a.Cbz32(cr)
	tail := f.a.Len()
	f.a.LoadIdx(X16, linMemReg, ar, 0, 8, false, true)
	f.a.Add64(sr, sr, X16)
	f.a.AddImm32(ar, ar, 8)
	f.a.SubsImm32(cr, cr, 1)
	back = f.a.Bcond(condNE)
	f.patchBranch19(back, tail)
	f.patchBranch19(first, f.a.Len())
	f.patchBranch19(empty, f.a.Len())
	for i := 1; i < chains; i++ {
		f.a.Add64(sr, sr, regs[i])
		f.pinned = f.pinned.remove(regs[i])
		f.release(regs[i])
	}
	f.markLocalDirty(counter)
	f.markLocalDirty(addr)
	f.markLocalDirty(acc)
	f.setFactsForLocal(counter, 0)
	f.linearSumLoop = 0
	f.linearSumLoopDepth = 0
	f.stats.peep("experimental-sum-group")
	return true
}
