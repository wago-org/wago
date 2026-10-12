//go:build arm64

package arm64

// wideArithmetic realizes each input before overwriting either result register.
// Input registers remain pinned while other deferred trees are materialized.
// The carry chain is adjacent, with no allocation or loads between its halves.
func (f *fn) wideArithmetic(sub uint32) {
	// Resolve earlier trapping expressions in source order before popping inputs.
	f.materializePendingTraps()
	saved := f.pinned
	count := 2
	if sub <= 20 {
		count = 4
	}
	var regs [4]Reg
	for i := count - 1; i >= 0; i-- {
		e := f.popValue()
		regs[i] = f.materialize(e)
		f.pinned = f.pinned.add(regs[i])
	}
	lo, hi := regs[0], regs[1]
	switch sub {
	case 19:
		f.a.Adds64(lo, lo, regs[2])
		f.a.Adc64(hi, hi, regs[3])
	case 20:
		f.a.Subs64(lo, lo, regs[2])
		f.a.Sbc64(hi, hi, regs[3])
	case 21, 22:
		// High must be computed before the low result destroys the left input.
		hi = f.allocReg(0)
		f.pinned = f.pinned.add(hi)
		if sub == 21 {
			f.a.Smulh(hi, regs[0], regs[1])
		} else {
			f.a.Umulh(hi, regs[0], regs[1])
		}
		f.a.Mul64(lo, regs[0], regs[1])
	}
	f.pinned = saved
	for i := 0; i < count; i++ {
		if regs[i] != lo && regs[i] != hi {
			f.release(regs[i])
		}
	}
	f.pushReg(lo, mtI64)
	f.pushReg(hi, mtI64)
}
