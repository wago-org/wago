//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// An if containing only a pure integer return has no fall-through work. Move
// its result construction to the inline function's existing cold-edge queue so
// the recursive loop continues without branching around the return block.
func (f *fn) tryInlineColdReturn(r *wasm.Reader) (bool, error) {
	if f.inlineTailLoopFrame == 0 || f.inlineRetFrame == 0 || f.makesCalls || !f.opt(optBranchFold) || profileEnabled || f.interruptible || f.moduleEH {
		return false, nil
	}
	frame := &f.ctrl[f.inlineRetFrame]
	condition := f.s.back()
	if !frame.has(ctrlRegMerge1) || frame.resultN != 1 || !f.tailSelectFlagsCondition(condition) || f.regUser[mergeReg] != nil {
		return false, nil
	}
	// Relocated result construction may read only constant/local inputs. The
	// condition therefore remains in its original local homes until its CMP.
	for _, node := range []*elem{f.s.arg0(condition), f.s.arg1(condition)} {
		if node != nil && node.st.kind != stLocalReg && node.st.kind != stConst {
			return false, nil
		}
	}
	for _, root := range f.rootsBottomToTop() {
		if root != condition && (root.elemKind() != ekValue || root.st.kind != stSlot) {
			return false, nil
		}
	}
	target := f.inlineTargets.target(int(f.traceFuncIdx))
	if target == nil {
		return false, nil
	}
	scan := *r
	expressions, ok := f.scanPureIntegerPacket(&scan, f.inlineTargets.localTypes(target), []machineType{frame.res0}, 0x0f, 0x0b)
	if !ok {
		return false, nil
	}
	saved, ok := f.snapshotLocalStates()
	if !ok {
		return false, nil
	}
	mark := f.a.Len()
	if err := f.emitTailSelectExpr(expressions[0]); err != nil {
		return true, err
	}
	result := f.s.back()
	f.condenseInto(result, mergeReg)
	f.erase(result)
	f.release(mergeReg)
	f.convergeBranchLocals(frame)
	code := append([]byte(nil), f.a.B[mark:]...)
	f.truncateNativeCode(mark)
	f.restoreLocalStates(saved)
	cc := f.condenseToFlags(condition)
	f.appendFrameColdEdge(frame, coldEdge{site: f.a.Bcond(cc), code: code})
	frame.set(ctrlEndReachable, true)
	if err := r.JumpTo(scan.Offset()); err != nil {
		return true, err
	}
	f.stats.peep("inline-cold-return")
	return true, nil
}
