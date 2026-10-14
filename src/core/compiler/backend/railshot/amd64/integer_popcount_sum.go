//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// For N=2^k+R, sum(popcnt(i), i<N) is k*2^(k-1)+R plus the
// corresponding sum below R. Each iteration removes N's highest set bit,
// bounding the reduction to 32/64 iterations, including unsigned extrema.
func (f *fn) emitIntegerPopcountSum(p shared.IntegerSumLoop, typ machineType, get func(uint32)) (bool, error) {
	f.pushValue(storage{kind: stConst, typ: typ, cval: 1})
	get(p.Bound)
	get(p.Bound)
	f.pushUnOp(opEqz, typ)
	f.emitSelect()
	f.setLocal(nil, int(p.Counter), false)
	f.flush()
	// Variable shifts require CL. Preserve any outstanding allocator owner
	// before claiming it, and keep all arithmetic scratch registers distinct.
	f.spillIfUsed(RCX)
	oldPinned := f.pinned
	f.pinned = f.pinned.add(RCX)
	get(p.Counter)
	input, owned := f.materializeRead(f.popValue())
	n := input
	if !owned {
		n = f.allocReg(maskOf(input))
		if p.Wide {
			f.a.MovReg64(n, input)
		} else {
			f.a.MovRegReg32(n, input)
		}
	} else if !p.Wide {
		// Arithmetic below is 64-bit; discard any upper bits of an i32 home.
		f.a.MovRegReg32(n, n)
	}
	sum := f.allocReg(maskOf(n))
	power := f.allocReg(maskOf(n, sum))
	f.a.MovImm32(sum, 0)
	loop := f.a.Len()
	// N is initially max(1, Bound), and the backedge requires N != 0.
	f.a.Bsr(RCX, n, true)
	f.a.MovImm32(power, 1)
	f.a.ShiftCL(4, power, true)
	f.a.AluRR(0x29, n, power, true)
	// Divide before multiplying: k=63 must retain the high bit of 2^k.
	f.a.ShiftImm(5, power, 1, true)
	f.a.IMul(power, RCX, true)
	f.a.AluRR(0x01, sum, power, true)
	f.a.AluRR(0x01, sum, n, true)
	f.a.TestSelf(n, true)
	back := f.a.JccPlaceholder(condNE)
	f.a.PatchRel32(back, loop)
	f.release(power)
	f.release(n)
	f.pinned = oldPinned
	f.release(RCX)
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
