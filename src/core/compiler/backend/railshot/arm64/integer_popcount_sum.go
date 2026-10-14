//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// For N=2^k+R, the population counts over [0,N) sum to k*2^(k-1)+R
// plus the sum over [0,R). Removing the highest bit on each iteration bounds
// descent to 32/64 iterations. Arithmetic wraps only in the final integer sum.
func (f *fn) emitIntegerPopcountSum(p shared.IntegerSumLoop, typ machineType, get func(uint32)) (bool, error) {
	f.pushValue(storage{kind: stConst, typ: typ, cval: 1})
	get(p.Bound)
	get(p.Bound)
	f.pushUnOp(opEqz, typ)
	f.emitSelect()
	f.setLocal(nil, int(p.Counter), false)
	f.flush()
	get(p.Counter)
	input, owned := f.materializeRead(f.popValue())
	var regs [6]Reg
	regs[0] = f.allocReg(maskOf(input))
	if p.Wide {
		f.a.MovReg64(regs[0], input)
	} else {
		f.a.MovReg32(regs[0], input)
	}
	if owned {
		f.release(input)
	}
	avoid := maskOf(regs[0])
	for i := 1; i < len(regs); i++ {
		regs[i] = f.allocReg(avoid)
		avoid = avoid.add(regs[i])
	}
	n, sum, k, power, one, top := regs[0], regs[1], regs[2], regs[3], regs[4], regs[5]
	f.a.MovImm64(sum, 0)
	f.a.MovImm64(one, 1)
	f.a.MovImm64(top, 63)
	loop := f.a.Len()
	f.a.Clz(k, n, false)
	f.a.Sub64(k, top, k)
	f.a.Lslv64(power, one, k)
	f.a.Sub64(n, n, power)
	// Shift the power first: multiplying before division would lose the top
	// bit at k=63. At k=0 the zero coefficient makes the half-power harmless.
	f.a.LsrImm(power, power, 1, false)
	f.a.Madd64(sum, power, k, sum)
	f.a.Add64(sum, sum, n)
	exit := f.a.Cbz64(n)
	back := f.a.Branch()
	f.patchBranch26(back, loop)
	f.patchBranch19(exit, f.a.Len())
	for _, reg := range regs {
		f.release(reg)
	}
	f.pushReg(sum, typ)
	get(p.Accumulator)
	f.pushBinOp(opAdd, typ)
	f.setLocal(nil, int(p.Accumulator), false)
	for _, x := range []uint32{p.Counter, p.Accumulator} {
		if x < 64 {
			f.localWritten |= uint64(1) << x
		}
	}
	f.stats.peep("integer-popcount-sum-loop")
	f.stats.peep("integer-sum-loop")
	return true, nil
}
