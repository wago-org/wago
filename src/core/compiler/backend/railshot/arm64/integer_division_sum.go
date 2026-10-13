//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// For N=q*D+r, sum(i/D)=D*T(q)+q*r, and sum(i%D)=T(N)-D*sum(i/D).
// Divide a triangular factor before multiplication to retain modulo-width
// arithmetic at unsigned extrema. All divisors are proven nonzero constants.
func (f *fn) emitIntegerDivisionSum(p shared.IntegerSumLoop, typ machineType, get func(uint32)) (bool, error) {
	constant := func(v uint64) { f.pushValue(storage{kind: stConst, typ: typ, cval: int64(v)}) }
	constant(1)
	get(p.Bound)
	get(p.Bound)
	f.pushUnOp(opEqz, typ)
	f.emitSelect()
	f.setLocal(nil, int(p.Counter), false)
	n := func() { get(p.Counter) }
	q := func() { n(); constant(p.Divisor); f.pushBinOp(opDivU, typ) }
	triangular := func(value func()) {
		value()
		constant(1)
		f.pushBinOp(opShrU, typ)
		value()
		constant(1)
		f.pushBinOp(opSub, typ)
		value()
		constant(1)
		f.pushBinOp(opAnd, typ)
		f.pushBinOp(opAdd, typ)
		f.pushBinOp(opMul, typ)
	}
	get(p.Accumulator)
	counterCoefficient := p.Coefficient + p.RemainderCoefficient
	quotientCoefficient := p.QuotientCoefficient - p.Divisor*p.RemainderCoefficient
	if counterCoefficient != 0 {
		triangular(n)
		constant(counterCoefficient)
		f.pushBinOp(opMul, typ)
		f.pushBinOp(opAdd, typ)
	}
	if quotientCoefficient != 0 {
		triangular(q)
		constant(p.Divisor)
		f.pushBinOp(opMul, typ)
		q()
		n()
		constant(p.Divisor)
		f.pushBinOp(opRemU, typ)
		f.pushBinOp(opMul, typ)
		f.pushBinOp(opAdd, typ)
		constant(quotientCoefficient)
		f.pushBinOp(opMul, typ)
		f.pushBinOp(opAdd, typ)
	}
	if p.Constant != 0 {
		n()
		constant(p.Constant)
		f.pushBinOp(opMul, typ)
		f.pushBinOp(opAdd, typ)
	}
	f.setLocal(nil, int(p.Accumulator), false)
	for _, x := range []uint32{p.Counter, p.Accumulator} {
		if x < 64 {
			f.localWritten |= uint64(1) << x
		}
	}
	f.stats.peep("integer-division-sum-loop")
	f.stats.peep("integer-sum-loop")
	return true, nil
}
