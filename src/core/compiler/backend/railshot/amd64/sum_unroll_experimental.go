//go:build amd64 && wago_sumunroll

package amd64

// Experimental builds only. Tests set this value before serial compilation.
// It is absent from normal builds and is not a production compiler option.
var sumUnrollExperiment struct {
	factor, chains, budget int
	hybrid                 bool
	threshold              int
	pairTail, reserve      bool
}

// Called once by the corpus test bridge, before any compilation or timing.
// This private setter exists only in experimental builds.
func setSumUnrollMeasurement(factor int, hybrid bool, threshold int) {
	sumUnrollExperiment.factor = factor
	sumUnrollExperiment.chains = 4
	sumUnrollExperiment.budget = 576
	sumUnrollExperiment.hybrid = hybrid
	sumUnrollExperiment.threshold = threshold
}

// Tests select mitigation modes once before compilation.
func setSumUnrollMitigation(pairTail, reserve bool) {
	sumUnrollExperiment.pairTail = pairTail
	sumUnrollExperiment.reserve = reserve
}

func (f *fn) trySelectedLinearSumLatch(loop *ctrlFrame, counter int) bool {
	c := sumUnrollExperiment
	if c.factor != 0 && f.tryLinearSumLatchMitigated(loop, counter, c.factor, c.chains, c.budget, c.hybrid, c.threshold, c.pairTail) {
		return true
	}
	return f.tryUnrolledLinearSumLatch(loop, counter)
}

// The scalar peel, range proof, wrapping dispatch, and tail match the existing
// emitter. Only grouping and the number of independent chains differ.
// A conservative encoding bound caps this latch at 512 bytes for 16/8.
func (f *fn) tryExperimentalLinearSumLatch(loop *ctrlFrame, counter int, factor, chains, budget int) bool {
	return f.tryLinearSumLatchWithTail(loop, counter, factor, chains, budget, false, 0)
}

// Hybrid groups share setup, wrap dispatch, scalar tail, and final combination.
func (f *fn) tryLinearSumLatchWithTail(loop *ctrlFrame, counter int, factor, chains, budget int, hybrid bool, threshold int) bool {
	return f.tryLinearSumLatchMitigated(loop, counter, factor, chains, budget, hybrid, threshold, false)
}

func (f *fn) tryLinearSumLatchMitigated(loop *ctrlFrame, counter int, factor, chains, budget int, hybrid bool, threshold int, pairTail bool) bool {
	if f.linearSumLoop == 0 || f.linearSumLoopDepth != uint16(len(f.ctrl)) {
		return false
	}
	addr := int(uint16(f.linearSumLoop) - 1)
	acc := int(uint16(f.linearSumLoop>>16) - 1)
	counterReg, counterFloat, counterPinned := f.pinReg(counter)
	addrReg, addrFloat, addrPinned := f.pinReg(addr)
	accReg, accFloat, accPinned := f.pinReg(acc)
	if !counterPinned || counterFloat || !addrPinned || addrFloat || !accPinned || accFloat {
		return false
	}

	bound := 256 + 8*factor + 16*chains
	if pairTail {
		bound += 32
	}
	fourTail := hybrid || threshold != 0
	if fourTail {
		bound += 64
	}
	if threshold != 0 {
		bound += 16
	}
	// Select only free registers. Do not spill or relinquish local pins. Failure
	// leaves assembler, allocator, and local state unchanged for the default path.
	if factor < 2 || factor > 16 || factor&(factor-1) != 0 ||
		chains < 2 || chains > 8 || chains > factor || factor%chains != 0 ||
		budget < bound || pairTail && (factor != 16 || chains != 4 || fourTail) || fourTail && (factor != 16 || chains != 4) ||
		threshold != 0 && (hybrid || threshold != 64 && threshold != 128 && threshold != 256) {
		return false
	}
	var regs [8]Reg
	regs[0] = accReg
	n := 1
	block := f.pinned.union(f.pinnedLocalMask).union(f.reserved).
		add(counterReg).add(addrReg).add(accReg)
	for _, r := range gpAlloc {
		if f.regUser[r] == nil && !block.has(r) {
			regs[n] = r
			block = block.add(r)
			n++
			if n == chains {
				break
			}
		}
	}
	if n != chains {
		return false
	}
	for i := 1; i < chains; i++ {
		f.pinned = f.pinned.add(regs[i])
		f.a.Xor32(regs[i], regs[i])
	}
	p1 := regs[1]
	// The scalar body already consumed the first element and advanced addr.
	f.a.AluRI(aluTable[opSub].digit, counterReg, 1, false)
	firstDone := f.a.JccPlaceholder(condE)
	minimum := factor
	if fourTail {
		minimum = 4
	}
	f.a.AluRI(cmpDigit, counterReg, int32(minimum), false)
	toRemainder := f.a.JccPlaceholder(condB)

	toWrapping := -1
	mt, _ := f.m.MemoryType(0)
	if !mt.Limits.HasMax || mt.Limits.Max >= 65536 {
		// Native group offsets do not wrap at 2^32. Select the scalar tail
		// once for wrapping ranges, keeping ordinary groups unchanged.
		f.a.MovRegReg32(p1, counterReg)
		f.a.ShiftImm(4, p1, 3, true)
		f.a.Add64(p1, addrReg)
		f.a.ShiftImm(5, p1, 32, true)
		f.a.MovImm64(p1, 0) // Preserve the shift's zero flag.
		toWrapping = f.a.JccPlaceholder(condNE)
	}

	toFour := -1
	if fourTail {
		// The wrap check precedes every group-offset load, including small
		// counts that could cross memory32. They must use the scalar tail.
		entryCount := factor
		if threshold != 0 {
			entryCount = threshold
		}
		f.a.AluRI(cmpDigit, counterReg, int32(entryCount), false)
		toFour = f.a.JccPlaceholder(condB)
	}
	group := f.a.Len()
	for i := 0; i < factor; i++ {
		f.a.AluIdx(aluTable[opAdd].rm, regs[i%chains], RBX, addrReg, int32(i*8), true)
	}
	f.a.AluRI(aluTable[opAdd].digit, addrReg, int32(factor*8), false)
	f.a.AluRI(aluTable[opSub].digit, counterReg, int32(factor), false)
	f.a.AluRI(cmpDigit, counterReg, int32(factor), false)
	moreGroups := f.a.JccPlaceholder(condAE)
	f.a.PatchRel32(moreGroups, group)

	if fourTail {
		// Hybrid large ranges use the four-element tail. The threshold design
		// instead preserves D's scalar tail on the large-range arm.
		toScalar := -1
		if hybrid {
			f.a.AluRI(cmpDigit, counterReg, 4, false)
			toScalar = f.a.JccPlaceholder(condB)
		} else {
			toScalar = f.a.JmpPlaceholder()
		}
		four := f.a.Len()
		f.a.PatchRel32(toFour, four)
		for i := 0; i < 4; i++ {
			f.a.AluIdx(aluTable[opAdd].rm, regs[i], RBX, addrReg, int32(i*8), true)
		}
		f.a.AluRI(aluTable[opAdd].digit, addrReg, 32, false)
		f.a.AluRI(aluTable[opSub].digit, counterReg, 4, false)
		f.a.AluRI(cmpDigit, counterReg, 4, false)
		moreFour := f.a.JccPlaceholder(condAE)
		f.a.PatchRel32(moreFour, four)
		f.a.PatchRel32(toScalar, f.a.Len())
	}
	f.a.PatchRel32(toRemainder, f.a.Len())
	if toWrapping >= 0 {
		f.a.PatchRel32(toWrapping, f.a.Len())
	}
	f.a.TestSelf(counterReg, false)
	noRemainder := f.a.JccPlaceholder(condE)
	oddTail := -1
	if pairTail {
		// Odd counts enter the second half, then all further visits consume pairs.
		// Each address increment remains 32-bit; neither load has a native offset.
		f.a.TestImm(counterReg, 1, false)
		oddTail = f.a.JccPlaceholder(condNE)
	}
	remainder := f.a.Len()
	if pairTail {
		f.a.AluIdx(aluTable[opAdd].rm, accReg, RBX, addrReg, 0, true)
		f.a.AluRI(aluTable[opAdd].digit, addrReg, 8, false)
		f.a.AluRI(aluTable[opSub].digit, counterReg, 1, false)
		f.a.PatchRel32(oddTail, f.a.Len())
	}
	tailAcc := accReg
	if pairTail {
		tailAcc = p1
	}
	f.a.AluIdx(aluTable[opAdd].rm, tailAcc, RBX, addrReg, 0, true)
	f.a.AluRI(aluTable[opAdd].digit, addrReg, 8, false)
	f.a.AluRI(aluTable[opSub].digit, counterReg, 1, false)
	moreRemainder := f.a.JccPlaceholder(condNE)
	f.a.PatchRel32(moreRemainder, remainder)

	combine := f.a.Len()
	f.a.PatchRel32(firstDone, combine)
	f.a.PatchRel32(noRemainder, combine)
	for i := 1; i < chains; i++ {
		f.a.Add64(accReg, regs[i])
	}
	for i := chains - 1; i >= 1; i-- {
		f.pinned = f.pinned.remove(regs[i])
		f.release(regs[i])
	}
	f.markLocalDirty(counter)
	f.stats.peep("experimental-linear-sum")
	if pairTail {
		f.stats.peep("experimental-linear-sum-pair-tail")
	}
	if hybrid {
		f.stats.peep("experimental-linear-sum-hybrid")
	}
	if threshold != 0 {
		f.stats.peep("experimental-linear-sum-threshold")
	}
	return true
}
