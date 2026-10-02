//go:build amd64

package amd64

const minAssociativeDestNeed = 4

// tryAssociativeTree covers a bounded, trap-free tree of one associative integer
// operation as a single accumulator. The existing Valent height bound is its
// fuel: unlike the old fixed eight-leaf array, every tree the stack can defer is
// eligible, and the two bounded walks allocate no scratch storage. Destination-
// hinted trees keep the established local-sink alias handling.
func (f *fn) tryAssociativeTree(node *elem, dest Reg) Reg {
	requestedDest := dest != regNone
	need := treeRegisterNeed(node)
	if !associativeOp(node.op) || need < 3 {
		return regNone
	}
	// A destination hint already removes the ordinary path's result copy. Spend
	// whole-tree selection only where the unflattened expression has materially
	// higher register pressure; need-three destination trees changed layout in hot
	// corpus functions without reducing spills and regressed their execution.
	if requestedDest && need < minAssociativeDestNeed {
		return regNone
	}
	n, first, _, ok := inspectAssociativeTree(node, node.op, node.typ)
	if !ok || n < 3 {
		return regNone
	}
	repeatedAlias := false
	if requestedDest {
		// A requested destination may still hold an input value. Select the one
		// flattened leaf that reads it as the accumulator seed. If several leaves
		// read dest only through borrowed local/global registers, preserve its old
		// value once and retarget the remaining reads to that pinned copy. Owned
		// register aliases stay on the ordinary path: changing those would also
		// require transferring allocator ownership.
		alias, count, replaceable := associativeDestLeaves(node, node.op, node.typ, dest)
		if count > 1 && !replaceable {
			return regNone
		}
		if count != 0 {
			first = alias
		}
		repeatedAlias = count > 1
	}
	f.stats.peep("assoc-tree-candidate")
	if !f.opt(optAssocTree) {
		return regNone
	}
	aliasCopy := regNone
	if repeatedAlias {
		aliasCopy = f.allocReg(maskOf(dest))
		f.moveInt(aliasCopy, dest, node.typ)
		f.pinned = f.pinned.add(aliasCopy)
		replaceAssociativeAliasLeaves(node, node.op, node.typ, first, dest, aliasCopy)
	}

	// Start with the most expensive leaf; every remaining leaf is then consumed
	// directly into the accumulator, so no internal binary result stays live.
	if dest == regNone {
		if first.kind == ekValue && first.st.kind == stReg {
			dest = first.st.reg
		} else {
			dest = f.allocReg(0)
		}
	}
	f.pinned = f.pinned.add(dest)
	f.condenseInto(first, dest)
	// A nested condense targeting dest removes its own pin before returning;
	// restore the accumulator pin before materializing another leaf.
	f.pinned = f.pinned.add(dest)
	f.applyAssociativeLeaves(node, node.op, node.typ, first, dest)
	f.pinned = f.pinned.remove(dest)
	if aliasCopy != regNone {
		f.pinned = f.pinned.remove(aliasCopy)
		f.stats.peep("assoc-tree-dest-repeat")
	}
	f.stats.peep("assoc-tree")
	if requestedDest {
		f.stats.peep("assoc-tree-dest")
	}
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	node.op = opNone
	return dest
}

// associativeDestLeaves counts flattened leaves whose subtree reads dest and
// reports whether all matching reads are borrowed pinned locals/globals. One
// aliasing leaf can safely seed an in-place accumulator. Several replaceable
// reads can share one saved copy without changing allocator ownership.
func associativeDestLeaves(e *elem, op wOp, typ machineType, dest Reg) (leaf *elem, count int, replaceable bool) {
	if e.kind == ekDeferred && e.op == op && e.typ == typ {
		left, ln, ld := associativeDestLeaves(e.arg0, op, typ, dest)
		right, rn, rd := associativeDestLeaves(e.arg1, op, typ, dest)
		if ln != 0 {
			return left, ln + rn, ld && rd
		}
		return right, rn, rd
	}
	if treeUsesReg(e, dest) {
		return e, 1, treeRegReplaceable(e, dest)
	}
	return nil, 0, true
}

func treeRegReplaceable(e *elem, reg Reg) bool {
	if e == nil {
		return true
	}
	if e.kind == ekValue {
		switch e.st.kind {
		case stReg:
			return e.st.reg != reg
		case stLocalReg, stGlobReg:
			return true
		default:
			return true
		}
	}
	return e.kind == ekDeferred &&
		treeRegReplaceable(e.arg0, reg) && treeRegReplaceable(e.arg1, reg)
}

func replaceAssociativeAliasLeaves(e *elem, op wOp, typ machineType, first *elem, from, to Reg) {
	if e.kind == ekDeferred && e.op == op && e.typ == typ {
		replaceAssociativeAliasLeaves(e.arg0, op, typ, first, from, to)
		replaceAssociativeAliasLeaves(e.arg1, op, typ, first, from, to)
		return
	}
	if e == first {
		return
	}
	replaceBorrowedTreeReg(e, from, to)
}

func replaceBorrowedTreeReg(e *elem, from, to Reg) {
	if e == nil {
		return
	}
	if e.kind == ekValue {
		if (e.st.kind == stLocalReg || e.st.kind == stGlobReg) && e.st.reg == from {
			e.st.reg = to
		}
		return
	}
	if e.kind == ekDeferred {
		replaceBorrowedTreeReg(e.arg0, from, to)
		replaceBorrowedTreeReg(e.arg1, from, to)
	}
}

func treeUsesReg(e *elem, reg Reg) bool {
	if e == nil {
		return false
	}
	if e.kind == ekValue {
		switch e.st.kind {
		case stReg, stLocalReg, stGlobReg:
			return e.st.reg == reg
		}
		return false
	}
	return e.kind == ekDeferred &&
		(treeUsesReg(e.arg0, reg) || treeUsesReg(e.arg1, reg))
}

// treeAccumulatorSafe is stricter than reorder safety: every covered leaf may
// be condensed while the accumulator is live. Variable shifts are excluded
// because their fixed RCX role can evict an accumulator that landed in RCX.
func treeAccumulatorSafe(e *elem) bool {
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
	if isShift(e.op) {
		if e.arg1 == nil || e.arg1.kind != ekValue || e.arg1.st.kind != stConst {
			return false
		}
	} else if !(isBinALU(e.op) || isCompare(e.op) || isUnary(e.op) || isConvert(e.op)) {
		return false
	}
	return treeAccumulatorSafe(e.arg0) && (e.arg1 == nil || treeAccumulatorSafe(e.arg1))
}

func associativeOp(op wOp) bool {
	switch op {
	case opAdd, opAnd, opOr, opXor:
		return true
	}
	return false
}

// inspectAssociativeTree validates accumulator safety and returns the leaf with
// the greatest stored register need. Work is bounded by maxDeferDepth.
func inspectAssociativeTree(e *elem, op wOp, typ machineType) (n int, first *elem, need int16, ok bool) {
	if e.kind == ekDeferred && e.op == op && e.typ == typ {
		ln, lf, lneed, lok := inspectAssociativeTree(e.arg0, op, typ)
		rn, rf, rneed, rok := inspectAssociativeTree(e.arg1, op, typ)
		if !lok || !rok {
			return 0, nil, 0, false
		}
		if rneed > lneed {
			lf, lneed = rf, rneed
		}
		return ln + rn, lf, lneed, true
	}
	if !treeAccumulatorSafe(e) {
		return 0, nil, 0, false
	}
	return 1, e, treeRegisterNeed(e), true
}

func (f *fn) applyAssociativeLeaves(e *elem, op wOp, typ machineType, first *elem, dest Reg) {
	if e.kind == ekDeferred && e.op == op && e.typ == typ {
		f.applyAssociativeLeaves(e.arg0, op, typ, first, dest)
		f.applyAssociativeLeaves(e.arg1, op, typ, first, dest)
		return
	}
	if e == first {
		return
	}
	if e.isDeferred() {
		// Nested lowering may temporarily pin and then unpin one of this cover's
		// protected registers. regMask is not reference-counted, so restore the
		// complete outer set before another leaf can allocate over it.
		protected := f.pinned
		f.condense(e, regNone)
		f.pinned = f.pinned.union(protected)
	}
	f.applyALU(aluTable[op], dest, e, typ.is64())
}
