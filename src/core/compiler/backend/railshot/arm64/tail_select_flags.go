//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type tailSelectCapture struct {
	left, right            *elem
	lreg, rreg, result     Reg
	lowned, rowned, common bool
	direct                 bool
	typ                    machineType
	saved                  storage
}

func (f *fn) tailSelectFlagsCondition(node *elem) bool {
	if !isFusableCondition(node) || isFloatCompare(node.deferredOp()) {
		return false
	}
	left := f.s.arg0(node)
	if left == nil || left.elemKind() != ekValue {
		return false
	}
	if node.deferredOp() == opEqz {
		return true
	}
	right := f.s.arg1(node)
	return right != nil && right.elemKind() == ekValue
}

// Capture every pure arm value before emitting the original compare. CSELs then
// share its flags directly: no boolean materialization or repeated CMP is needed.
func (f *fn) emitTailSelectOnFlags(r *wasm.Reader, end int, left, right [4]tailSelectExpr, target *inlineTarget, condition *elem) (bool, error) {
	previousPins, previousFloor := f.pinned, f.spillFloor
	defer func() { f.pinned = previousPins; f.spillFloor = previousFloor }()
	var captures [4]tailSelectCapture
	for i := 0; i < target.paramCount(); i++ {
		capture := &captures[i]
		capture.typ = left[i].typ
		if err := f.emitTailSelectExpr(left[i]); err != nil {
			return true, err
		}
		capture.left = f.s.back()
		if bytes.Equal(left[i].bytes(), right[i].bytes()) {
			capture.common = true
			if capture.left.isDeferred() {
				f.materialize(capture.left)
			}
			if capture.left.st.kind == stReg {
				f.pinned = f.pinned.add(capture.left.st.reg)
			}
			continue
		}
		capture.lreg, capture.lowned = f.materializeRead(capture.left)
		f.pinned = f.pinned.add(capture.lreg)
		if err := f.emitTailSelectExpr(right[i]); err != nil {
			return true, err
		}
		capture.right = f.s.back()
		capture.rreg, capture.rowned = f.materializeRead(capture.right)
		f.pinned = f.pinned.add(capture.rreg)
		if capture.lowned {
			capture.result = capture.lreg
		} else if capture.rowned {
			capture.result = capture.rreg
		} else {
			capture.result = f.allocReg(maskOf(capture.lreg, capture.rreg))
		}
		f.pinned = f.pinned.add(capture.result)
	}
	// A parameter home can receive its selected value directly when no other
	// selection still reads that home. Cross-argument permutations keep their
	// captured scratch destinations and the ordinary parallel assignment path.
	for i := 0; i < target.paramCount(); i++ {
		capture := &captures[i]
		if capture.common {
			continue
		}
		dest, fp, ok := f.pinReg(f.localBase + i)
		if !ok || fp {
			continue
		}
		used := false
		for j := 0; j < target.paramCount(); j++ {
			if i == j {
				continue
			}
			other := &captures[j]
			if other.common {
				st := other.left.st
				used = used || (st.kind == stReg || st.kind == stLocalReg) && st.reg == dest || st.kind == stLocalRef && st.index() == f.localBase+i
			} else {
				used = used || other.lreg == dest || other.rreg == dest
			}
		}
		if used {
			continue
		}
		if capture.result != capture.lreg && capture.result != capture.rreg {
			f.release(capture.result)
		}
		capture.result, capture.direct = dest, true
	}
	// Removing the condition and rebuilding the argument roots changes logical
	// slot positions. Keep later spills above every still-live captured slot.
	f.spillFloor = max(f.spillFloor, f.maxSpill)
	preparedPins := f.pinned
	var cc Cond
	if f.tailSelectFlagsCondition(condition) {
		cc = f.condenseToFlags(condition)
	} else {
		reg := f.materialize(condition)
		f.a.CmpImm32(reg, 0)
		f.erase(condition)
		f.release(reg)
		cc = condNE
	}
	f.pinned = preparedPins
	for i := 0; i < target.paramCount(); i++ {
		capture := &captures[i]
		if !capture.common {
			f.a.Csel(capture.result, capture.lreg, capture.rreg, cc, capture.typ.is64())
		}
	}
	for i := 0; i < target.paramCount(); i++ {
		capture := &captures[i]
		if capture.common {
			capture.saved = capture.left.st
		} else {
			if capture.lowned && capture.lreg != capture.result {
				f.release(capture.lreg)
			}
			if capture.rowned && capture.rreg != capture.result && capture.rreg != capture.lreg {
				f.release(capture.rreg)
			}
			f.erase(capture.right)
		}
		f.erase(capture.left)
	}
	for i := 0; i < target.paramCount(); i++ {
		capture := &captures[i]
		if capture.common {
			root := f.pushValue(capture.saved)
			if root.st.kind == stReg {
				f.regUser[root.st.reg] = root
			}
		} else {
			var root *elem
			if capture.direct {
				local := f.localBase + i
				f.invalidateLocalWriteFacts(local)
				f.setFactsForLocal(local, 0)
				f.markLocalDirty(local)
				root = f.pushValue(storage{kind: stLocalReg, typ: capture.typ, reg: capture.result, idx: uint32(local)})
				f.stats.peep("tail-select-parameter-home")
			} else {
				root = f.pushReg(capture.result, capture.typ)
			}
			if capture.typ == mtI32 {
				root.st.setValueFacts(factUpper32Zero)
			}
		}
	}
	f.pinned = previousPins
	if err := r.JumpTo(end); err != nil {
		return true, err
	}
	f.stats.peep("tail-branch-select")
	f.stats.peep("tail-branch-select-flags")
	return true, f.returnCallIndex(f.traceFuncIdx)
}
