//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

func (f *fn) emitIntegerByteSum(p shared.IntegerSumLoop, typ machineType, get func(uint32)) (bool, error) {
	constant := func(v int64) { f.pushValue(storage{kind: stConst, typ: typ, cval: v}) }
	constant(1)
	get(p.Bound)
	get(p.Bound)
	f.pushUnOp(opEqz, typ)
	f.emitSelect()
	f.setLocal(nil, int(p.Counter), false)
	// Each complete period contributes 0+...+255 = 32640. The remaining
	// r*(r-1) fits within 16 bits, so dividing that product cannot lose a bit.
	get(p.Accumulator)
	get(p.Counter)
	constant(8)
	f.pushBinOp(opShrU, typ)
	constant(32640)
	f.pushBinOp(opMul, typ)
	remainder := func() {
		get(p.Counter)
		constant(255)
		f.pushBinOp(opAnd, typ)
	}
	remainder()
	constant(1)
	f.pushBinOp(opSub, typ)
	remainder()
	f.pushBinOp(opMul, typ)
	constant(1)
	f.pushBinOp(opShrU, typ)
	f.pushBinOp(opAdd, typ)
	f.pushBinOp(opAdd, typ)
	f.setLocal(nil, int(p.Accumulator), false)
	for _, x := range []uint32{p.Counter, p.Accumulator} {
		if x < 64 {
			f.localWritten |= uint64(1) << x
		}
	}
	f.stats.peep("integer-byte-sum-loop")
	f.stats.peep("integer-sum-loop")
	return true, nil
}
