//go:build arm64

package arm64

// realizeWideOperands leaves operands on the stack while evaluating deferred
// expressions. Fixed-register lowerings in a later operand may spill earlier
// results, which must remain visible to the allocator until evaluation ends.
func (f *fn) realizeWideOperands(n int) {
	var roots [4]*elem
	e := f.s.back()
	for i := n - 1; i >= 0; i-- {
		roots[i] = e
		e = f.s.prev(f.s.baseOfValentBlock(e))
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
		f.a.Subs64(regs[0], regs[0], regs[2])
		f.a.Sbc64(regs[1], regs[1], regs[3])
	} else {
		f.a.Adds64(regs[0], regs[0], regs[2])
		f.a.Adc64(regs[1], regs[1], regs[3])
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
	right := f.materialize(f.popValue())
	f.pinned = f.pinned.add(right)
	left := f.materialize(f.popValue())
	f.pinned = f.pinned.add(left)
	high := f.allocReg(maskOf(left, right))
	// Compute the high half before destructively replacing the left operand.
	if signed {
		f.a.Smulh(high, left, right)
	} else {
		f.a.Umulh(high, left, right)
	}
	f.a.Mul64(left, left, right)
	f.release(right)
	f.pinned = f.pinned.remove(left).remove(right)
	f.pushReg(left, mtI64)
	f.pushReg(high, mtI64)
}
