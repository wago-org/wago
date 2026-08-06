//go:build amd64

package amd64

const maxAssociativeLeaves = 8

// tryAssociativeTree covers a small, trap-free tree of one associative integer
// operation as a single accumulator. The cover is fixed-size and allocation-free.
// Destination-hinted trees keep the established local-sink alias handling.
func (f *fn) tryAssociativeTree(node *elem, dest Reg) Reg {
	if dest != regNone || !associativeOp(node.op) || treeRegisterNeed(node) < 3 || !treeAccumulatorSafe(node) {
		return regNone
	}
	var leaves [maxAssociativeLeaves]*elem
	n := 0
	if !collectAssociativeLeaves(node, node.op, node.typ, leaves[:], &n) || n < 3 {
		return regNone
	}
	f.stats.peep("assoc-tree-candidate")
	if !associativeTreeEnabled {
		return regNone
	}

	// Start with the most expensive leaf; every remaining leaf is then consumed
	// directly into the accumulator, so no internal binary result stays live.
	first := 0
	firstNeed := treeRegisterNeed(leaves[0])
	for i := 1; i < n; i++ {
		if need := treeRegisterNeed(leaves[i]); need > firstNeed {
			first, firstNeed = i, need
		}
	}
	leaves[0], leaves[first] = leaves[first], leaves[0]

	if leaves[0].kind == ekValue && leaves[0].st.kind == stReg {
		dest = leaves[0].st.reg
	} else {
		dest = f.allocReg(0)
	}
	f.pinned = f.pinned.add(dest)
	f.condenseInto(leaves[0], dest)
	// A nested condense targeting dest removes its own pin before returning;
	// restore the accumulator pin before materializing another leaf.
	f.pinned = f.pinned.add(dest)
	for i := 1; i < n; i++ {
		leaf := leaves[i]
		if leaf.isDeferred() {
			f.condense(leaf, regNone)
			f.pinned = f.pinned.add(dest)
		}
		f.applyALU(aluTable[node.op], dest, leaf, node.typ.is64())
	}
	f.pinned = f.pinned.remove(dest)
	f.stats.peep("assoc-tree")
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	node.op = opNone
	return dest
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

func collectAssociativeLeaves(e *elem, op wOp, typ machineType, leaves []*elem, n *int) bool {
	if e.kind == ekDeferred && e.op == op && e.typ == typ {
		return collectAssociativeLeaves(e.arg0, op, typ, leaves, n) &&
			collectAssociativeLeaves(e.arg1, op, typ, leaves, n)
	}
	if *n == len(leaves) {
		return false
	}
	leaves[*n] = e
	*n = *n + 1
	return true
}
