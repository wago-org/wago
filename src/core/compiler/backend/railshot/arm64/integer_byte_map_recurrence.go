//go:build arm64

package arm64

// The caller proves the complete output span, nonwrapping indices, the affine
// relation between both producers, and 1<=divisor<=240. Only the stored low
// byte needs a quotient here; final locals use full-width GP arithmetic.
func (f *fn) emitByteMapRecurrence(p integerByteMap, gp [4]Reg, vr [8]Reg) {
	a, b, c, d := gp[0], gp[1], gp[2], gp[3]
	quotientStep, scaled, divisor, temp, remainder, quotient, remainderStep, scaledStep := vr[0], vr[1], vr[2], vr[3], vr[4], vr[5], vr[6], vr[7]
	constant := func(v Reg, value func(uint32) byte) {
		var words [2]uint64
		for lane := uint32(0); lane < 16; lane++ {
			words[lane/8] |= uint64(value(lane)) << ((lane % 8) * 8)
		}
		f.a.MovImm64(X16, words[0])
		f.a.NeonDupGprD(v, X16)
		f.a.MovImm64(X16, words[1])
		f.a.NeonInsD(v, X16, 1)
	}
	constant(quotientStep, func(lane uint32) byte { return byte(lane) })
	constant(remainder, func(lane uint32) byte { return byte(lane % p.divisor) })
	constant(quotient, func(lane uint32) byte { return byte(lane / p.divisor) })
	f.a.MovImm64(X16, uint64(p.divisor))
	f.a.NeonDupGprB(divisor, X16)
	f.a.Udiv32(X17, a, X16)
	f.a.Msub32(X16, X17, X16, a)
	f.a.NeonDupGprB(temp, X17)
	f.a.NeonAddB(quotient, quotient, temp)
	f.a.NeonDupGprB(temp, X16)
	f.a.NeonAddB(remainder, remainder, temp)
	f.a.NeonCmhsB(temp, remainder, divisor)
	f.a.NeonSubB(quotient, quotient, temp)
	f.a.NeonAnd16b(temp, temp, divisor)
	f.a.NeonSubB(remainder, remainder, temp)
	f.a.NeonDupGprB(remainderStep, d)
	f.a.NeonMulB(scaled, quotientStep, remainderStep)
	f.a.NeonDupGprB(temp, c)
	f.a.NeonAddB(scaled, scaled, temp)
	f.a.MovImm64(X16, 16)
	f.a.Mul32(X16, X16, d)
	f.a.NeonDupGprB(scaledStep, X16)
	f.a.MovImm64(X16, uint64(16%p.divisor))
	f.a.NeonDupGprB(remainderStep, X16)
	f.a.MovImm64(X16, uint64(16/p.divisor))
	f.a.NeonDupGprB(quotientStep, X16)
	// Each source iteration produces two bytes. Keep one to eight source
	// iterations for the unchanged scalar tail and loop-exit state.
	f.a.MovReg32(a, b)
	f.a.MovImm64(X16, uint64(p.limit))
	f.a.Sub32(b, X16, b)
	f.a.LsrImm32(b, b, 1)
	f.a.SubImm32(b, b, 1)
	f.a.LsrImm32(b, b, 3)
	f.a.MovImm64(X16, uint64(p.offset))
	f.a.Add64(a, a, X16)
	f.a.Add64(a, a, linMemReg)
	header := f.a.Len()
	f.a.NeonAddB(temp, quotient, scaled)
	f.a.StrQ(a, 0, temp)
	if 16%p.divisor != 0 {
		f.a.NeonAddB(remainder, remainder, remainderStep)
		f.a.NeonCmhsB(temp, remainder, divisor)
		f.a.NeonSubB(quotient, quotient, temp)
		f.a.NeonAnd16b(temp, temp, divisor)
		f.a.NeonSubB(remainder, remainder, temp)
	}
	if 16/p.divisor != 0 {
		f.a.NeonAddB(quotient, quotient, quotientStep)
	}
	f.a.NeonAddB(scaled, scaled, scaledStep)
	f.a.AddImm64(a, a, 16)
	f.a.SubsImm32(b, b, 1)
	f.patchBranch19(f.a.Bcond(condNE), header)
	// Recover full-width final locals independently of the byte quotients.
	// No original local has been changed yet, so its old index remains valid.
	f.a.Sub64(a, a, linMemReg)
	f.a.MovImm64(X16, uint64(p.offset))
	f.a.Sub64(a, a, X16)
	f.a.SubImm32(a, a, 1)
	if r, _, pin := f.pinReg(p.even); pin {
		f.a.MovReg32(b, r)
	} else {
		f.ld32(b, SP, f.localOff(p.even))
	}
	f.a.Sub32(b, a, b)
	f.a.Madd32(c, b, d, c)
	f.a.Add32(d, c, d)
	f.a.AddImm32(b, a, 1)
	f.stats.peep("integer-quotient-recurrence")
}
