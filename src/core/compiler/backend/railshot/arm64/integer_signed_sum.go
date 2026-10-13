//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// Signed narrowing of A*i+B has sum A*T(N)+B*N-M*floorSum(N,M,A,B+M/2),
// where M=2^bits and coefficients are first reduced modulo M. The difference
// between the biased and unbiased floor sums counts negative narrowed values.
func (f *fn) emitIntegerSignedSum(p shared.IntegerSumLoop, typ machineType, get func(uint32)) (bool, error) {
	constant := func(v uint64) { f.pushValue(storage{kind: stConst, typ: typ, cval: int64(v)}) }
	constant(1)
	get(p.Bound)
	get(p.Bound)
	f.pushUnOp(opEqz, typ)
	f.emitSelect()
	f.setLocal(nil, int(p.Counter), false)
	m := uint64(1) << p.SignedBits
	a, b := p.Coefficient&(m-1), p.Constant&(m-1)
	get(p.Accumulator)
	if p.NegativeCount {
		f.emitIntegerFloorSum(p.Counter, m, a, b, get)
		f.emitIntegerFloorSum(p.Counter, m, a, b+m/2, get)
		f.pushBinOp(opSub, typ)
	} else {
		get(p.Counter)
		constant(1)
		f.pushBinOp(opShrU, typ)
		get(p.Counter)
		constant(1)
		f.pushBinOp(opSub, typ)
		get(p.Counter)
		constant(1)
		f.pushBinOp(opAnd, typ)
		f.pushBinOp(opAdd, typ)
		f.pushBinOp(opMul, typ)
		constant(a)
		f.pushBinOp(opMul, typ)
		get(p.Counter)
		constant(b)
		f.pushBinOp(opMul, typ)
		f.pushBinOp(opAdd, typ)
		f.emitIntegerFloorSum(p.Counter, m, a, b+m/2, get)
		constant(m)
		f.pushBinOp(opMul, typ)
		f.pushBinOp(opSub, typ)
	}
	f.pushBinOp(opAdd, typ)
	f.setLocal(nil, int(p.Accumulator), false)
	for _, x := range []uint32{p.Counter, p.Accumulator} {
		if x < 64 {
			f.localWritten |= uint64(1) << x
		}
	}
	f.stats.peep("integer-signed-sum-loop")
	f.stats.peep("integer-sum-loop")
	return true, nil
}

// Compute sum floor((A*i+B)/M), i=0..N-1, by Euclidean descent. N starts
// at at most 2^32-1, M at at most 2^32, and A<M, B<3*M/2. All products fit
// unsigned 64 bits. Each iteration reduces the positive modulus; at most 64
// iterations suffice for these widths. This uses no memory, helpers or calls.
func (f *fn) emitIntegerFloorSum(counter uint32, modulus, coefficient, offset uint64, get func(uint32)) {
	f.flush()
	get(counter)
	input, owned := f.materializeRead(f.popValue())
	var regs [8]Reg
	regs[0] = f.allocReg(maskOf(input))
	f.a.MovReg32(regs[0], input)
	if owned {
		f.release(input)
	}
	avoid := maskOf(regs[0])
	for i := 1; i < len(regs); i++ {
		regs[i] = f.allocReg(avoid)
		avoid = avoid.add(regs[i])
	}
	n, m, a, b, sum, q, y, t := regs[0], regs[1], regs[2], regs[3], regs[4], regs[5], regs[6], regs[7]
	f.a.MovImm64(m, modulus)
	f.a.MovImm64(a, coefficient)
	f.a.MovImm64(b, offset)
	f.a.MovImm64(sum, 0)
	loop := f.a.Len()
	f.a.Udiv64(q, a, m)
	f.a.SubImm64(t, n, 1)
	f.a.Mul64(t, t, n)
	f.a.LsrImm(t, t, 1, false)
	f.a.Madd64(sum, t, q, sum)
	f.a.Msub64(a, q, m, a)
	f.a.Udiv64(q, b, m)
	f.a.Madd64(sum, n, q, sum)
	f.a.Msub64(b, q, m, b)
	exit := f.a.Cbz64(a)
	f.a.Madd64(y, a, n, b)
	f.a.Udiv64(n, y, m)
	// With the reduced numerator below M, every remaining contribution is
	// zero. Avoid descending the rest of the modulus chain for small bounds.
	zero := f.a.Cbz64(n)
	f.a.Msub64(b, n, m, y)
	f.a.MovReg64(t, m)
	f.a.MovReg64(m, a)
	f.a.MovReg64(a, t)
	back := f.a.Branch()
	f.a.PatchBranch26(back, loop)
	f.a.PatchBranch19(exit, f.a.Len())
	f.a.PatchBranch19(zero, f.a.Len())
	for _, reg := range regs {
		f.release(reg)
	}
	f.pushReg(sum, mtI32)
}
