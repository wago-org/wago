//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// skipDroppedGCConstructor consumes an immediately following drop and removes
// operandN constructor arguments from the compiler stack without allocating the
// object. All constructor arguments have already been evaluated in Wasm order.
// Non-trapping deferred trees can disappear completely; if any argument still
// carries a deferred trap, flush preserves the original bottom-to-top order.
func (f *fn) skipDroppedGCConstructor(r *wasm.Reader, operandN int) bool {
	if !deadGCNewEnabled {
		return false
	}
	op, ok := r.Peek()
	if !ok || op != 0x1a { // drop
		return false
	}
	_, _ = r.Byte() // Peek proved this byte exists.
	f.discardGCConstructorOperands(operandN)
	f.retireGCFrameSafepoint()
	f.stats.peep("gc-dead-new")
	return true
}

// deferGCConstructorForDroppedStruct replaces a constructor result with a null
// placeholder only when bounded lookahead proves that the result flows untouched
// into a following struct.new whose result is immediately dropped. The outer
// struct constructor will be removed by skipDroppedGCConstructor, so the
// placeholder is never observed as a Wasm value.
func (f *fn) deferGCConstructorForDroppedStruct(r *wasm.Reader, operandN int) bool {
	if !deadGCNewEnabled || !f.gcConstructorFeedsDroppedStruct(r) {
		return false
	}
	f.discardGCConstructorOperands(operandN)
	f.retireGCFrameSafepoint()
	f.pushValue(storage{kind: stConst, typ: mtI64}) // private null placeholder
	f.stats.peep("gc-dead-new")
	return true
}

// gcConstructorFeedsDroppedStruct recognizes a deliberately small postfix
// shape: the current constructor result, followed by zero or more push-only
// leaves, then struct.new and drop. Push-only leaves cannot consume or expose the
// candidate reference. This covers Dew's fixed vector -> dropped wrapper trees
// without constructing an instruction graph or retaining body-wide IR.
func (f *fn) gcConstructorFeedsDroppedStruct(r *wasm.Reader) bool {
	look := *r
	above := 0
	const maxLookaheadInstructions = 32
	for range maxLookaheadInstructions {
		op, err := look.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0x20, 0x23, 0xd2: // local.get, global.get, ref.func
			if _, err := look.U32(); err != nil {
				return false
			}
			above++
		case 0x41: // i32.const
			if _, err := look.I32(); err != nil {
				return false
			}
			above++
		case 0x42: // i64.const
			if _, err := look.I64(); err != nil {
				return false
			}
			above++
		case 0x43: // f32.const
			if _, err := look.LEU32(); err != nil {
				return false
			}
			above++
		case 0x44: // f64.const
			if _, err := look.LEU64(); err != nil {
				return false
			}
			above++
		case 0xd0: // ref.null
			if _, err := look.S33(); err != nil {
				return false
			}
			above++
		case 0xfd: // v128.const
			sub, err := look.U32()
			if err != nil || sub != 12 {
				return false
			}
			if _, err := look.Bytes(16); err != nil {
				return false
			}
			above++
		case 0xfb:
			sub, err := look.U32()
			if err != nil || sub != 0 { // struct.new only
				return false
			}
			typeIndex, err := look.U32()
			if err != nil {
				return false
			}
			st, ok := stagedStructType(f.m, typeIndex)
			if !ok || len(st.Comp.Fields) <= above {
				// The outer constructor does not consume the candidate.
				return false
			}
			next, err := look.Byte()
			return err == nil && next == 0x1a
		default:
			return false
		}
	}
	return false
}

// retireGCFrameSafepoint consumes the frontend's liveness entry for an
// allocation removed by this pass. Keeping the dense ID preserves every later
// site's compiler/runtime identity; no native call can publish the retired ID.
func (f *fn) retireGCFrameSafepoint() {
	plan := f.gcFrameRoots
	if plan == nil || !plan.Candidate {
		return
	}
	index := len(plan.Safepoints)
	id := plan.SafepointBase + uint32(index+1)
	if index >= len(plan.LiveLocalMasks) || id == 0 || id > shared.GCSafepointIDMax {
		plan.Exact = false
		return
	}
	plan.Safepoints = append(plan.Safepoints, shared.GCFrameSafepointPlan{ID: id})
}

func (f *fn) discardGCConstructorOperands(n int) {
	if n == 0 {
		return
	}
	roots := f.rootsBottomToTop()
	if n < 0 || n > len(roots) {
		panic("amd64: invalid dead GC constructor operand count")
	}
	dead := roots[len(roots)-n:]
	for _, root := range dead {
		if !f.gcTreeDiscardable(root) {
			// Preserve deferred trap order. flush walks all roots bottom-to-top,
			// exactly like the constructor helper call this replaces.
			f.flush()
			for range n {
				f.erase(f.s.back())
			}
			return
		}
	}
	for i := len(dead) - 1; i >= 0; i-- {
		f.discardGCTree(dead[i])
	}
}

func (f *fn) gcTreeDiscardable(e *elem) bool {
	if e == nil || e.kind == ekSkip || e.kind == ekBlock {
		return false
	}
	if e.kind == ekDeferred {
		return !isDivRem(e.op) && f.gcTreeDiscardable(e.arg0) &&
			(e.arg1 == nil || f.gcTreeDiscardable(e.arg1))
	}
	if e.st.ehRoot {
		return false
	}
	// In guard mode the deferred load itself is the bounds trap. Explicit mode
	// has already emitted the check, so the unconsumed load is side-effect free.
	return e.st.kind != stMemRef || !f.guardMode
}

func (f *fn) discardGCTree(e *elem) {
	if e.kind == ekDeferred {
		if e.arg1 != nil {
			f.discardGCTree(e.arg1)
		}
		f.discardGCTree(e.arg0)
		f.erase(e)
		return
	}
	switch e.st.kind {
	case stReg:
		if e.st.typ == mtCustom {
			for _, reg := range e.st.vregs {
				f.releaseF(reg)
			}
		} else if e.st.typ.isXMM() {
			f.releaseF(e.st.reg)
		} else {
			f.release(e.st.reg)
		}
	case stMemRef:
		f.releaseMemRef(e.st)
	}
	f.erase(e)
}
