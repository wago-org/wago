//go:build amd64

package amd64

import x86 "github.com/wago-org/wago/src/core/encoder/amd64"

// emitRoundSSE2 rounds the low scalar lane without consulting or changing
// MXCSR. Integer IEEE-754 decomposition handles even subnormals and signaling
// NaNs without FP arithmetic. The result preserves the sign of zero and quiets
// NaNs, as required by Wasm rounding. Upper XMM lanes are unspecified.
//
// bits, magnitude, mask, temporary and RCX are distinct, caller-owned GPRs.
// dst and src may alias. No stack space, runtime helper or heap is needed.
func emitRoundSSE2(a *x86.Asm, dst, src, bits, magnitude, mask, temporary Reg, f64 bool, mode byte) {
	mantissa, bias, sign := byte(23), int32(127), byte(31)
	if f64 {
		mantissa, bias, sign = 52, 1023, 63
	}
	var done [8]int
	nDone := 0
	finish := func(cc x86.Cond) {
		done[nDone] = a.JccPlaceholder(cc)
		nDone++
	}
	jumpDone := func() {
		done[nDone] = a.JmpPlaceholder()
		nDone++
	}
	imm := func(r Reg, v uint64) {
		if f64 {
			a.MovImm64(r, v)
		} else {
			a.MovImm32(r, int32(v))
		}
	}
	a.MovXmmToGpr(bits, src, f64)
	a.AluRR(0x89, magnitude, bits, f64)
	a.ShiftImm(4, magnitude, 1, f64)
	a.ShiftImm(5, magnitude, 1, f64) // remove sign
	a.AluRR(0x89, temporary, magnitude, f64)
	a.ShiftImm(5, temporary, mantissa, f64)
	a.AluRI(7, temporary, bias+int32(mantissa), false)
	large := a.JccPlaceholder(x86.CondAE)
	a.AluRI(7, temporary, bias, false)
	small := a.JccPlaceholder(x86.CondB)

	// 1 <= |x| < 2^mantissa. The fractional field has between 1 and
	// mantissa bits. Clearing it yields truncation without a conversion.
	a.MovImm32(RCX, bias+int32(mantissa))
	a.AluRR(0x29, RCX, temporary, false)
	a.MovImm32(mask, 1)
	a.ShiftCL(4, mask, f64)
	a.AluRI(5, mask, 1, f64)
	a.AluRR(0x89, temporary, magnitude, f64)
	a.AluRR(0x21, temporary, mask, f64)
	finish(x86.CondE)                   // integral input
	a.AluRR(0x31, bits, temporary, f64) // clear fractional bits
	switch mode & 3 {
	case 0: // nearest, ties to even
		a.AluRI(0, mask, 1, f64)
		a.ShiftImm(5, mask, 1, f64)
		a.AluRR(0x39, temporary, mask, f64)
		finish(x86.CondB)
		increment := a.JccPlaceholder(x86.CondA)
		a.AluRR(0x89, temporary, bits, f64)
		a.ShiftCL(5, temporary, f64)
		a.TestImm(temporary, 1, false)
		finish(x86.CondE)
		a.PatchRel32(increment, a.Len())
		a.ShiftImm(4, mask, 1, f64)
		a.AluRR(0x01, bits, mask, f64)
	case 1, 2: // floor / ceil: increase magnitude only in the chosen direction
		a.TestSelf(bits, f64)
		if mode&3 == 1 {
			finish(x86.CondS ^ 1)
		} else {
			finish(x86.CondS)
		}
		a.AluRI(0, mask, 1, f64)
		a.AluRR(0x01, bits, mask, f64)
	}
	jumpDone()

	a.PatchRel32(small, a.Len())
	// |x| < 1: keep only the sign and choose between signed zero and one.
	a.ShiftImm(5, bits, sign, f64)
	a.ShiftImm(4, bits, sign, f64)
	switch mode & 3 {
	case 0:
		imm(mask, uint64(bias-1)<<mantissa) // exact 0.5
		a.AluRR(0x39, magnitude, mask, f64)
		finish(x86.CondBE) // ties at +/-0.5 go to signed zero
	case 1, 2:
		a.TestSelf(magnitude, f64)
		finish(x86.CondE)
		a.TestSelf(bits, f64)
		if mode&3 == 1 {
			finish(x86.CondS ^ 1)
		} else {
			finish(x86.CondS)
		}
	}
	if mode&3 != 3 {
		imm(mask, uint64(bias)<<mantissa)
		a.AluRR(0x09, bits, mask, f64)
	}
	jumpDone()

	a.PatchRel32(large, a.Len())
	// Large finite inputs and infinities are already integral. Quiet any NaN
	// using its payload bits so this path cannot overflow or raise FP flags.
	imm(mask, uint64(2*bias+1)<<mantissa)
	a.AluRR(0x39, magnitude, mask, f64)
	finish(x86.CondBE)
	imm(mask, uint64(1)<<(mantissa-1))
	a.AluRR(0x09, bits, mask, f64)
	for _, off := range done[:nDone] {
		a.PatchRel32(off, a.Len())
	}
	a.MovGprToXmm(dst, bits, f64)
}
