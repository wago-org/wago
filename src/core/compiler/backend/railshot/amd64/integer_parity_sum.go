//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// With N=max(1,Bound), the even indices have M=ceil(N/2) elements and
// sum M*(M-1). The odd indices have K=floor(N/2) elements and sum K*K.
// Every multiply/add uses i32 modular arithmetic, matching each source term
// and accumulation, including unsigned bounds and signed coefficients.
func (f *fn) emitIntegerParitySum(p shared.IntegerSumLoop, typ machineType, get func(uint32)) {
	constant := func(v uint64) { f.pushValue(storage{kind: stConst, typ: typ, cval: int64(v)}) }
	oddCount := func() { get(p.Bound); constant(1); f.pushBinOp(opShrU, typ) }
	evenCount := func() {
		oddCount()
		get(p.Bound)
		constant(1)
		f.pushBinOp(opAnd, typ)
		f.pushBinOp(opAdd, typ)
		get(p.Bound)
		f.pushUnOp(opEqz, typ)
		f.pushBinOp(opAdd, typ)
	}
	scale := func(v uint64) {
		if v != 1 {
			constant(v)
			f.pushBinOp(opMul, typ)
		}
	}
	get(p.Accumulator)
	if p.EvenCoefficient != 0 {
		evenCount()
		evenCount()
		constant(1)
		f.pushBinOp(opSub, typ)
		f.pushBinOp(opMul, typ)
		scale(p.EvenCoefficient)
		f.pushBinOp(opAdd, typ)
	}
	if p.EvenConstant != 0 {
		evenCount()
		scale(p.EvenConstant)
		f.pushBinOp(opAdd, typ)
	}
	if p.OddCoefficient != 0 {
		oddCount()
		oddCount()
		f.pushBinOp(opMul, typ)
		scale(p.OddCoefficient)
		f.pushBinOp(opAdd, typ)
	}
	if p.OddConstant != 0 {
		oddCount()
		scale(p.OddConstant)
		f.pushBinOp(opAdd, typ)
	}
	f.stats.peep("integer-parity-sum-loop")
}
