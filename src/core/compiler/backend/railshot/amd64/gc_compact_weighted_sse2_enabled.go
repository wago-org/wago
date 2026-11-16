//go:build amd64 && (wago_gc_compact_weighted_sse2 || wago_gc_direct_weighted_sse2)

package amd64

const gcCompactWeightedSSE2 = true

// Full-span proof and the original checked first iteration precede this call.
// Only accumulator lanes 0 and 2 are meaningful: each collects the low dword
// of one even and one odd PMULUDQ product. PADDD cannot carry between lanes.
func (f *fn) emitCompactWeightedSSE2(l directLoopLease, index, total, offset, value Reg, payload int32) int {
	if f.weightedBound < 4 {
		return -1
	}
	var regs [6]Reg
	avoid := regMask(0)
	for i := range regs {
		r := f.freeFReg(avoid)
		if r == regNone {
			return -1
		}
		regs[i] = r
		avoid = avoid.add(r)
	}
	pins := f.fpinned
	f.fpinned = f.fpinned.union(avoid)
	weights, data, oddWeights, oddData, sum, step := regs[0], regs[1], regs[2], regs[3], regs[4], regs[5]
	f.a.AluRI(cmpDigit, index, int32(f.weightedBound-4), false)
	short := f.a.JccPlaceholder(condA)
	// Build [0,1,2,3] without a literal pool, memory scratch or optional ISA.
	f.a.MovImm64(value, 0x0000000100000000)
	f.a.MovGprToXmm(data, value, true)
	f.a.MovImm64(value, 0x0000000300000002)
	f.a.MovGprToXmm(oddData, value, true)
	f.a.Punpcklqdq(data, oddData)
	f.a.LeaDispW(value, index, f.weightedWeight, false)
	f.a.MovGprToXmm(weights, value, false)
	f.a.Pshufd(weights, weights, 0)
	f.a.SseMapRR(0x66, 0, 0xfe, weights, data) // PADDD
	f.a.MovImm32(value, 4)
	f.a.MovGprToXmm(step, value, false)
	f.a.Pshufd(step, step, 0)
	f.a.SseMapRR(0x66, 0, 0xef, sum, sum) // PXOR
	f.a.MovRegReg32(offset, index)
	f.a.ImulRI(offset, 4, true)
	loop := f.a.Len()
	f.a.MovdquLoadIdx(data, l.object, offset, payload)
	// Duplicate odd lanes into even positions without changing source weights.
	f.a.Pshufd(oddData, data, 0xf5)
	f.a.Pshufd(oddWeights, weights, 0xf5)
	f.a.SseMapRR(0x66, 0, 0xf4, data, weights) // PMULUDQ
	f.a.SseMapRR(0x66, 0, 0xf4, oddData, oddWeights)
	f.a.SseMapRR(0x66, 0, 0xfe, sum, data)
	f.a.SseMapRR(0x66, 0, 0xfe, sum, oddData)
	f.a.SseMapRR(0x66, 0, 0xfe, weights, step)
	f.a.AluRI(0, offset, 16, true)
	f.a.AluRI(0, index, 4, false)
	f.a.AluRI(cmpDigit, index, int32(f.weightedBound-4), false)
	again := f.a.JccPlaceholder(condBE)
	f.a.PatchRel32(again, loop)
	f.a.Pshufd(data, sum, 0xaa) // Lane 2, not discarded high-product lanes.
	f.a.SseMapRR(0x66, 0, 0xfe, sum, data)
	f.a.MovXmmToGpr(value, sum, false)
	f.a.AluRR(0x01, total, value, false)
	f.a.AluRI(cmpDigit, index, int32(f.weightedBound), false)
	done := f.a.JccPlaceholder(condE)
	f.a.PatchRel32(short, f.a.Len())
	f.fpinned = pins
	for _, r := range regs {
		f.releaseF(r)
	}
	if l.kind == loopLeaseTrusted {
		f.stats.peep("gc-direct-weighted-sse2")
	} else {
		f.stats.peep("gc-compact-weighted-sse2")
	}
	return done
}
