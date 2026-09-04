//go:build amd64

package amd64

// treeRegisterNeed returns the incrementally stored Sethi-Ullman label for a
// deferred tree. Synthetic nodes in focused tests may omit the label, in which
// case the same bounded calculation is performed on demand.
func treeRegisterNeed(e *elem) int16 {
	if e == nil || !e.isDeferred() {
		return 1
	}
	if e.registerNeed() != 0 {
		return e.registerNeed()
	}
	left := treeRegisterNeed(e.arg0)
	right := treeRegisterNeed(e.arg1)
	return combineRegisterNeed(left, right, e.arg1 != nil)
}

func combineRegisterNeed(left, right int16, binary bool) int16 {
	if !binary {
		return left
	}
	if left == right {
		return left + 1
	}
	if left > right {
		return left
	}
	return right
}

// labelDeferredNode records the bounded labels that are properties of a node's
// tree shape. Keep this as the single construction seam for future selector
// labels (effects, target forms, and competing costs).
func labelDeferredNode(e *elem) {
	e.setDeferredDepth(1 + max16(deferDepthOf(e.arg0), deferDepthOf(e.arg1)))
	e.setRegisterNeed(combineRegisterNeed(treeRegisterNeed(e.arg0), treeRegisterNeed(e.arg1), e.arg1 != nil))
}

// treeReorderSafe excludes any leaf or operation whose evaluation can trap.
// Reordering such work would violate Wasm's precise left-to-right trap order.
// stMemRef is excluded even though its bounds check may already have run: in
// guard mode the deferred native load itself remains the trapping operation.
func treeReorderSafe(e *elem) bool {
	if e == nil {
		return false
	}
	if e.isValue() {
		switch e.st.kind {
		case stConst, stReg, stSlot, stLocalRef, stLocalReg, stGlobalRef, stGlobReg:
			return true
		default:
			return false
		}
	}
	if !e.isDeferred() {
		return false
	}
	if !(isBinALU(e.deferredOp()) || isShift(e.deferredOp()) || isCompare(e.deferredOp()) || isUnary(e.deferredOp()) || isConvert(e.deferredOp())) {
		return false
	}
	return treeReorderSafe(e.arg0) && (e.arg1 == nil || treeReorderSafe(e.arg1))
}
