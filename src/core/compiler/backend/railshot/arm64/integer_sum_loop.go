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
	p, ok := shared.InspectIntegerSumLoopForModule(r, f.m, f.opt(optInline))
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
	if p.InvariantGlobal {
		gt, ok := f.globalType(p.Global)
		if !ok {
			return false, nil
		}
		if gt.Mutable {
			// Reuse the existing native value pin: the original call-free loop
			// already reads this stable register, not the global cell each time.
			if _, pinned := f.pinnedGlobalValueReg(p.Global); !pinned {
				return false, nil
			}
		}
	}
	// Original source, not fused-emitter write history, proves initialization.
	// An enclosing loop could reenter here after a later counter assignment.
	for _, frame := range f.ctrl {
		if frame.kind == cfLoop {
			return false, nil
		}
	}
	if int(p.Counter) < f.nParams || !shared.IntegerSumCounterUntouched(r, p.Counter, f.classifier) {
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
	if p.Coefficient != 1 {
		f.pushValue(storage{kind: stConst, typ: typ, cval: int64(p.Coefficient)})
		f.pushBinOp(opMul, typ)
	}
	if p.Constant != 0 || p.InvariantGlobal {
		if p.InvariantGlobal {
			if err := f.globalGetIndex(p.Global); err != nil {
				return false, err
			}
		} else {
			f.pushValue(storage{kind: stConst, typ: typ, cval: int64(p.Constant)})
		}
		// The constant term is contributed once even when Bound is zero.
		one()
		get(p.Bound)
		get(p.Bound)
		f.pushUnOp(opEqz, typ)
		f.emitSelect()
		f.pushBinOp(opMul, typ)
		f.pushBinOp(opAdd, typ)
	}
	f.pushBinOp(opAdd, typ)
	f.setLocal(nil, int(p.Accumulator), false)
	// A do-while loop executes once even for n=0, so its final counter is 1.
	one()
	get(p.Bound)
	get(p.Bound)
	f.pushUnOp(opEqz, typ)
	f.emitSelect()
	f.setLocal(nil, int(p.Counter), false)
	// Publish every final global through canonical setters. All reads in the
	// original body were dominated by same-iteration writes; no prior global
	// value or aliased imported cell participates in this proof.
	for i := uint8(0); i < p.GlobalWriteCount; i++ {
		w := p.GlobalWrites[i]
		get(p.Counter)
		one()
		f.pushBinOp(opSub, typ)
		if w.Coefficient != 1 {
			f.pushValue(storage{kind: stConst, typ: typ, cval: int64(w.Coefficient)})
			f.pushBinOp(opMul, typ)
		}
		if w.Constant != 0 {
			f.pushValue(storage{kind: stConst, typ: typ, cval: int64(w.Constant)})
			f.pushBinOp(opAdd, typ)
		}
		if err := f.globalSetIndex(w.Index); err != nil {
			return false, err
		}
	}
	if p.GlobalWriteCount != 0 {
		f.stats.peep("integer-sum-global-writes")
	}

	for _, x := range []uint32{p.Counter, p.Accumulator} {
		if x < 64 {
			f.localWritten |= uint64(1) << x
		}
	}
	if p.Coefficient != 1 || p.Constant != 0 {
		f.stats.peep("integer-affine-sum-loop")
	}
	if p.Calls != 0 {
		f.stats.peep("integer-sum-pure-call")
	}
	if p.InvariantGlobal {
		f.stats.peep("integer-sum-invariant-global")
	}
	f.stats.peep("integer-sum-loop")
	return true, nil
}
