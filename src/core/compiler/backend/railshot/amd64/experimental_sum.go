//go:build amd64

package amd64

// This emitter keeps the original scalar peel, register allocator and range
// proof. Its only choices are the group size and number of dependency chains.
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
		f.a.Xor32(regs[i], regs[i])
	}
	// A separate scratch register leaves the one-chain case comparable. It has
	// the same allocation policy in every experimental group shape.
	scratch := f.allocReg(0)
	f.pinned = f.pinned.add(scratch)
	f.a.AluRI(aluTable[opSub].digit, cr, 1, false)
	first := f.a.JccPlaceholder(condE)
	f.a.AluRI(cmpDigit, cr, int32(factor), false)
	short := f.a.JccPlaceholder(condB)
	wrap := -1
	mt, _ := f.m.MemoryType(0)
	if !mt.Limits.HasMax || mt.Limits.Max >= 65536 {
		f.a.MovRegReg32(scratch, cr)
		f.a.ShiftImm(4, scratch, 3, true)
		f.a.Add64(scratch, ar)
		f.a.ShiftImm(5, scratch, 32, true)
		wrap = f.a.JccPlaceholder(condNE)
	}
	group := f.a.Len()
	for i := 0; i < factor; i++ {
		f.a.AluIdx(aluTable[opAdd].rm, regs[i%chains], RBX, ar, int32(i*8), true)
	}
	f.a.AluRI(aluTable[opAdd].digit, ar, int32(factor*8), false)
	f.a.AluRI(aluTable[opSub].digit, cr, int32(factor), false)
	f.a.AluRI(cmpDigit, cr, int32(factor), false)
	back := f.a.JccPlaceholder(condAE)
	f.a.PatchRel32(back, group)
	f.a.PatchRel32(short, f.a.Len())
	if wrap >= 0 {
		f.a.PatchRel32(wrap, f.a.Len())
	}
	f.a.TestSelf(cr, false)
	empty := f.a.JccPlaceholder(condE)
	tail := f.a.Len()
	f.a.AluIdx(aluTable[opAdd].rm, sr, RBX, ar, 0, true)
	f.a.AluRI(aluTable[opAdd].digit, ar, 8, false)
	f.a.AluRI(aluTable[opSub].digit, cr, 1, false)
	back = f.a.JccPlaceholder(condNE)
	f.a.PatchRel32(back, tail)
	f.a.PatchRel32(first, f.a.Len())
	f.a.PatchRel32(empty, f.a.Len())
	for i := 1; i < chains; i++ {
		f.a.Add64(sr, regs[i])
		f.pinned = f.pinned.remove(regs[i])
		f.release(regs[i])
	}
	f.pinned = f.pinned.remove(scratch)
	f.release(scratch)
	f.markLocalDirty(counter)
	f.stats.peep("experimental-sum-group")
	return true
}
