//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var integerSumLoopEnabled = true

func (f *fn) tryIntegerSumLoop(r *wasm.Reader) (bool, error) {
	if !f.opt(optIntegerSumLoop) || f.unreachable || f.interruptible || f.localBase != 0 || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 {
		return false, nil
	}
	p, ok := shared.InspectIntegerSumLoop(r)
	if !ok {
		return false, nil
	}
	typ := mtI32
	if p.Wide {
		typ = mtI64
	}
	for _, x := range []uint32{p.Counter, p.Accumulator, p.Bound} {
		if uint64(x) >= uint64(len(f.localType)) || f.localType[x] != typ {
			return false, nil
		}
	}
	// Untouched declared locals start at zero even when eager initialization
	// does not retain a constant state. Exclude enclosing loops: a later write
	// in their body could change this local before a subsequent entry here.
	for _, frame := range f.ctrl {
		if frame.kind == cfLoop {
			return false, nil
		}
	}
	untouched := p.Counter < 64 && int(p.Counter) >= f.nParams && f.localWritten&(uint64(1)<<p.Counter) == 0
	if !f.localConstZero(int(p.Counter)) && !untouched {
		return false, nil
	}
	if err := r.JumpTo(p.End); err != nil {
		return false, err
	}
	// Resolve earlier traps and borrowed stack values before modifying locals or
	// emitting the retained barrier. No temporary source positions are fabricated.
	f.flush()
	f.invalidateStoreForward()
	if p.Fenced {
		f.a.DmbIsh()
	}
	get := func(x uint32) {
		if reg, _, ok := f.pinReg(int(x)); ok {
			f.recoverLocal(int(x))
			f.pushValue(storage{kind: stLocalReg, typ: typ, reg: reg, idx: x})
		} else if f.localConstZero(int(x)) {
			f.pushValue(zeroStorage(typ))
		} else {
			f.pushValue(storage{kind: stLocalRef, typ: typ, idx: x})
		}
	}
	one := func() { f.pushValue(storage{kind: stConst, typ: typ, cval: 1}) }
	// T(n)=floor(n/2)*(n-1+(n&1)). Dividing a factor before multiplying
	// preserves the complete modulo-2^32/2^64 result, including unsigned extrema.
	get(p.Accumulator)
	get(p.Bound)
	one()
	f.pushBinOp(opShrU, typ)
	get(p.Bound)
	one()
	f.pushBinOp(opSub, typ)
	get(p.Bound)
	one()
	f.pushBinOp(opAnd, typ)
	f.pushBinOp(opAdd, typ)
	f.pushBinOp(opMul, typ)
	f.pushBinOp(opAdd, typ)
	f.setLocal(nil, int(p.Accumulator), false)
	// A do-while loop executes once even for n=0, so its final counter is 1.
	one()
	get(p.Bound)
	get(p.Bound)
	f.pushUnOp(opEqz, typ)
	f.emitSelect()
	f.setLocal(nil, int(p.Counter), false)
	for _, x := range []uint32{p.Counter, p.Accumulator} {
		if x < 64 {
			f.localWritten |= uint64(1) << x
		}
	}
	f.stats.peep("integer-sum-loop")
	return true, nil
}
