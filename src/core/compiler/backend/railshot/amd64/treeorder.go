//go:build amd64

package amd64

// treeRegisterNeed returns a Sethi-Ullman-style register requirement for the
// bounded deferred tree rooted at e. Values need one register. Equal-sized
// children need one extra register because one result must remain live while
// the other is emitted. Valent already caps tree height, so this walk is O(1)
// with respect to function size and needs no stored annotation.
func treeRegisterNeed(e *elem) int16 {
	if e == nil || e.kind != ekDeferred {
		return 1
	}
	left := treeRegisterNeed(e.arg0)
	right := treeRegisterNeed(e.arg1)
	if e.arg1 == nil {
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
