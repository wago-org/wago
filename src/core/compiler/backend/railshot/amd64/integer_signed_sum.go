//go:build amd64

package amd64

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

// Euclidean descent computes sum floor((A*i+B)/M), i<N. Its initial
// N <= 2^32-1, M <= 2^32, A<M and B<3*M/2 keep each division numerator
// within uint64. The positive modulus decreases at every backedge.
// Frame scratch homes keep the native division's fixed AX/DX pair available
// without requiring eight simultaneously free general-purpose registers.
func (f *fn) emitIntegerFloorSum(counter uint32, modulus, coefficient, offset uint64, get func(uint32)) {
	f.flush()
	f.spillIfUsed(RAX)
	f.spillIfUsed(RDX)
	oldPinned := f.pinned
	f.pinned = f.pinned.add(RAX).add(RDX)
	get(counter)
	input, owned := f.materializeRead(f.popValue())
	x := f.allocReg(maskOf(input))
	y := f.allocReg(maskOf(input, x))
	// All allocator operations finish before these raw scratch homes go live.
	// They may be reused after the reduction, but cannot overlap live operands.
	slot := f.allocSpillSlots(5)
	n, m, a, b, sum := f.spillOff(slot), f.spillOff(slot+1), f.spillOff(slot+2), f.spillOff(slot+3), f.spillOff(slot+4)
	f.a.MovRegReg32(RAX, input)
	f.a.Store64(RSP, n, RAX)
	if owned {
		f.release(input)
	}
	for _, v := range []struct {
		off int32
		val uint64
	}{{m, modulus}, {a, coefficient}, {b, offset}, {sum, 0}} {
		f.a.MovImm64(x, v.val)
		f.a.Store64(RSP, v.off, x)
	}
	divide := func() {
		f.a.AluRR(0x31, RDX, RDX, false)
		f.a.Load64(x, RSP, m)
		f.a.Div(x, true)
	}
	loop := f.a.Len()
	f.a.Load64(RAX, RSP, a)
	divide()
	f.a.Store64(RSP, a, RDX)
	f.a.Load64(y, RSP, n)
	f.a.AluRI(5, y, 1, true)
	f.a.Load64(x, RSP, n)
	f.a.IMul(y, x, true)
	f.a.ShiftImm(5, y, 1, true)
	f.a.IMul(y, RAX, true)
	f.a.Load64(x, RSP, sum)
	f.a.AluRR(0x01, x, y, true)
	f.a.Store64(RSP, sum, x)
	f.a.Load64(RAX, RSP, b)
	divide()
	f.a.Store64(RSP, b, RDX)
	f.a.Load64(y, RSP, n)
	f.a.IMul(y, RAX, true)
	f.a.Load64(x, RSP, sum)
	f.a.AluRR(0x01, x, y, true)
	f.a.Store64(RSP, sum, x)
	f.a.Load64(x, RSP, a)
	f.a.TestSelf(x, true)
	zeroCoefficient := f.a.JccPlaceholder(condE)
	f.a.Load64(RAX, RSP, n)
	f.a.IMul(RAX, x, true)
	f.a.Load64(y, RSP, b)
	f.a.AluRR(0x01, RAX, y, true)
	divide()
	f.a.Store64(RSP, n, RAX)
	f.a.TestSelf(RAX, true)
	zeroBound := f.a.JccPlaceholder(condE)
	f.a.Store64(RSP, b, RDX)
	f.a.Load64(y, RSP, a)
	f.a.Store64(RSP, m, y)
	f.a.Store64(RSP, a, x)
	back := f.a.JmpPlaceholder()
	f.a.PatchRel32(back, loop)
	f.a.PatchRel32(zeroCoefficient, f.a.Len())
	f.a.PatchRel32(zeroBound, f.a.Len())
	f.a.Load64(y, RSP, sum)
	f.release(x)
	f.pinned = oldPinned
	f.release(RAX)
	f.release(RDX)
	f.pushReg(y, mtI32)
}
