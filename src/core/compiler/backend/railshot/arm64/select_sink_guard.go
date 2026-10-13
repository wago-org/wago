//go:build arm64

package arm64

import "os"

var selectSinkPureGuardEnabled = os.Getenv("WAGO_ARM64_NO_SELECT_SINK_PURE_GUARD") != "1"

// Pure integer work can be skipped when a select keeps its destination local.
// Trapping loads/divides and unsupported nodes decline. The bounded weighted
// cost avoids branching around a single cheap operation.
func pureSelectSinkCost(s *stack, e *elem, budget int) (int, bool) {
	if e == nil || budget <= 0 || (e.st.typ != mtI32 && e.st.typ != mtI64) {
		return 0, false
	}
	if e.elemKind() == ekValue {
		switch e.st.kind {
		case stConst, stReg, stSlot, stLocalRef, stLocalReg, stGlobReg:
			return 0, true
		}
		return 0, false
	}
	if !e.isDeferred() {
		return 0, false
	}
	cost := 1
	switch e.deferredOp() {
	case opMul:
		cost = 4
	case opAdd, opSub, opAnd, opOr, opXor, opShl, opShrS, opShrU, opRotl, opRotr:
	default:
		return 0, false
	}
	a, ok := pureSelectSinkCost(s, s.arg0(e), budget-1)
	if !ok {
		return 0, false
	}
	b, ok := pureSelectSinkCost(s, s.arg1(e), budget-1)
	return a + b + cost, ok
}

func (f *fn) tryPureSelectSinkGuard(a, b, cond *elem, x int, dest Reg, wide bool) bool {
	if !f.opt(optSelectSinkPureGuard) || len(f.customInstructions) != 0 || (!a.isDeferred() && !b.isDeferred()) || x < f.nParams || (wide && f.gcFrameLocal(x)) || (!wide && !f.canonicalI32Local(x)) || f.usesCalls || f.intervalControl && f.intervalActive != 0 || !isFusableCondition(cond) {
		return false
	}
	keepsDest := func(e *elem) bool {
		return e.elemKind() == ekValue && e.st.kind == stLocalReg && e.st.index() == x && e.st.reg == dest
	}
	work, keep := a, b
	skipTrue := false
	if !keepsDest(b) || !a.isDeferred() {
		if !keepsDest(a) || !b.isDeferred() {
			return false
		}
		work, keep = b, a
		skipTrue = true
	}
	cost, ok := pureSelectSinkCost(f.s, work, 5)
	if !ok || cost < 5 {
		return false
	}
	if f.s.prev(f.s.baseOfValentBlock(a)) != f.s.head {
		f.flushBelow(a)
	} else {
		f.invalidateGlobalsCache()
		f.invalidateBoundsCert()
	}
	f.invalidateStoreForward()
	cc := f.condenseToFlags(cond)
	skipCond := invertCond(cc)
	if skipTrue {
		skipCond = cc
	}
	skip := f.a.Bcond(skipCond)

	f.condenseInto(work, dest)
	f.release(dest)
	f.erase(work)
	f.erase(keep)
	f.patchBranch19(skip, f.a.Len())
	f.invalidateGlobalsCache()
	f.invalidateStoreForward()
	f.invalidateBoundsCert()
	f.stats.peep("select-sink-pure-guard")
	return true
}
