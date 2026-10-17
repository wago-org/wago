//go:build amd64

package amd64

// wideArithmetic emits native carry chains and signed/unsigned RDX:RAX
// multiplication. All inputs are realized before the arithmetic flags are set.
func (f *fn) wideArithmetic(sub uint32) {
	// Resolve earlier trapping expressions in source order before popping inputs.
	f.materializePendingTraps()
	saved := f.pinned
	count := 4
	if sub >= 21 {
		count = 2
		f.spillIfUsed(RAX)
		f.spillIfUsed(RDX)
		f.pinned = f.pinned.add(RAX).add(RDX)
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
		f.a.AluRR(0x01, lo, regs[2], true) // add
		f.a.AluRR(0x11, hi, regs[3], true) // adc
	case 20:
		f.a.AluRR(0x29, lo, regs[2], true) // sub
		f.a.AluRR(0x19, hi, regs[3], true) // sbb
	case 21, 22:
		f.a.MovReg64(RAX, regs[0])
		if sub == 21 {
			f.a.IMulHigh(regs[1], true)
		} else {
			f.a.Mul(regs[1], true)
		}
		lo, hi = RAX, RDX
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
