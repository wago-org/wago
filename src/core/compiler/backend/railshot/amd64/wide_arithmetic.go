//go:build amd64

package amd64

// realizeWideOperands leaves operands on the stack while evaluating deferred
// expressions. Fixed-register lowerings in a later operand may spill earlier
// results, which must remain visible to the allocator until evaluation ends.
func (f *fn) realizeWideOperands(n int) {
	var roots [4]*elem
	e := f.s.back()
	for i := n - 1; i >= 0; i-- {
		roots[i] = e
		e = baseOfValentBlock(e).prev
	}
	for i := 0; i < n; i++ {
		if roots[i].isDeferred() {
			f.condense(roots[i], regNone)
		}
	}
}

func (f *fn) wideAddSub(sub bool) {
	f.realizeWideOperands(4)
	var regs [4]Reg
	for i := 3; i >= 0; i-- {
		regs[i] = f.materialize(f.popValue())
		f.pinned = f.pinned.add(regs[i])
	}
	if sub {
		f.a.AluRR(0x29, regs[0], regs[2], true) // SUB low
		f.a.AluRR(0x19, regs[1], regs[3], true) // SBB high
	} else {
		f.a.AluRR(0x01, regs[0], regs[2], true) // ADD low
		f.a.AluRR(0x11, regs[1], regs[3], true) // ADC high
	}
	for _, r := range regs {
		f.pinned = f.pinned.remove(r)
	}
	f.release(regs[2])
	f.release(regs[3])
	f.pushReg(regs[0], mtI64)
	f.pushReg(regs[1], mtI64)
}

func (f *fn) wideMul(signed bool) {
	f.realizeWideOperands(2)
	// MUL/IMUL write RDX:RAX. Home any live occupants before reserving
	// them, including operands that the evaluation above put there.
	f.spillIfUsed(RAX)
	f.spillIfUsed(RDX)
	f.pinned = f.pinned.add(RAX).add(RDX)
	right := f.materialize(f.popValue())
	f.pinned = f.pinned.add(right)
	left := f.materialize(f.popValue())
	f.a.MovReg64(RAX, left)
	if signed {
		f.a.IMulHigh(right, true)
	} else {
		f.a.Mul(right, true)
	}
	f.release(left)
	f.release(right)
	f.pinned = f.pinned.remove(right).remove(RAX).remove(RDX)
	f.pushReg(RAX, mtI64)
	f.pushReg(RDX, mtI64)
}
