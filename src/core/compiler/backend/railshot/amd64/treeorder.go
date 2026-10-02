//go:build amd64

package amd64

// treeRegisterNeed returns the incrementally stored Sethi-Ullman label for a
// deferred tree. Synthetic nodes in focused tests may omit the label, in which
// case the same bounded calculation is performed on demand.
func treeRegisterNeed(e *elem) int16 {
	if e == nil || e.kind != ekDeferred {
		return 1
	}
	if e.regNeed != 0 {
		return e.regNeed
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
	e.deferDepth = 1 + max16(deferDepthOf(e.arg0), deferDepthOf(e.arg1))
	e.regNeed = combineRegisterNeed(treeRegisterNeed(e.arg0), treeRegisterNeed(e.arg1), e.arg1 != nil)
}

// treeReorderSafe excludes any leaf or operation whose evaluation can trap.
// Reordering such work would violate Wasm's precise left-to-right trap order.
// stMemRef is excluded even though its bounds check may already have run: in
// guard mode the deferred native load itself remains the trapping operation.
func treeReorderSafe(e *elem) bool {
	if e == nil {
		return false
	}
	if e.kind == ekValue {
		switch e.st.kind {
		case stConst, stReg, stSlot, stLocalRef, stLocalReg, stGlobalRef, stGlobReg:
			return true
		default:
			return false
		}
	}
	if e.kind != ekDeferred {
		return false
	}
	if !(isBinALU(e.op) || isShift(e.op) || isCompare(e.op) || isUnary(e.op) || isConvert(e.op)) {
		return false
	}
	return treeReorderSafe(e.arg0) && (e.arg1 == nil || treeReorderSafe(e.arg1))
}
