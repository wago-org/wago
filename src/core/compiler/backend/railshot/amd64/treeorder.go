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
	leftDepth, leftNeed := storedTreeLabels(e.arg0)
	rightDepth, rightNeed := storedTreeLabels(e.arg1)
	if leftNeed == 0 {
		leftNeed = treeRegisterNeed(e.arg0)
	}
	if rightNeed == 0 {
		rightNeed = treeRegisterNeed(e.arg1)
	}
	depth := uint8(1 + max16(leftDepth, rightDepth))
	need := uint8(combineRegisterNeed(leftNeed, rightNeed, e.arg1 != nil))
	e.st.slot = e.st.slot&^0x00ffff00 | uint32(depth)<<8 | uint32(need)<<16
}

// A deferred child's labels occupy adjacent bytes of the same word. Load them
// together; zero demand retains the recursive fallback for unlabelled nodes.
// Leaves (including the absent right child of a unary node) need one register.
func storedTreeLabels(e *elem) (depth, need int16) {
	if e == nil || !e.isDeferred() {
		return 0, 1
	}
	labels := e.st.slot
	return int16(uint8(labels >> 8)), int16(uint8(labels >> 16))
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
	if !(isBinALU(e.deferredOp()) || isShift(e.deferredOp()) || isCompare(e.deferredOp()) || e.deferredOp() == opEqz || isUnary(e.deferredOp()) || isConvert(e.deferredOp())) {
		return false
	}
	return treeReorderSafe(e.arg0) && (e.arg1 == nil || treeReorderSafe(e.arg1))
}

// materializeTrapsBefore preserves earlier trapping operands before a boundary
// evaluates its condition or arguments. Leave pure trees deferred and keep the
// existing nodes spillable; canonicalizing slots here could overwrite an
// argument that still needs to be captured from a noncanonical spill slot.
func (f *fn) materializeTrapsBefore(node *elem) {
	base := baseOfValentBlock(node)
	for e := f.s.head.next; e != base; e = e.next {
		if e.st.hasLogicalRoot() && !treeReorderSafe(e) {
			f.materializeByType(e)
		}
	}
}
