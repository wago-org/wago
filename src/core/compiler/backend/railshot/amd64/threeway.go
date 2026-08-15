//go:build amd64

package amd64

// tryThreeWayUnsignedCompare lowers
//
//	(a >u b) - (a <u b)
//
// to one CMP whose flags feed SETA and SBB. SETcc preserves flags, so SBB with
// zero maps less/equal/greater to -1/0/1 without materializing and subtracting
// two booleans. Reversing the two comparisons is handled by swapping a and b.
// The matcher accepts only repeated simple values: folding mutable loads would
// change their observable read count in shared-memory programs.
func (f *fn) tryThreeWayUnsignedCompare(node *elem, dest Reg) Reg {
	left, right, ok := matchThreeWayUnsignedCompare(node)
	if !ok {
		return regNone
	}
	f.stats.peep("three-way-unsigned-candidate")
	if !f.opt(optSTFlags) {
		return regNone
	}

	// Reserve a requested sink until both operands have been read. It may be a
	// pinned local that is also one of the inputs and must not be overwritten by
	// materialization before CMP.
	destWasPinned := dest != regNone && f.pinned.has(dest)
	if dest != regNone {
		f.pinned = f.pinned.add(dest)
	}
	l := f.materialize(left)
	f.pinned = f.pinned.add(l)
	r := f.materialize(right)
	f.pinned = f.pinned.add(r)
	result := dest
	if result == regNone {
		result = f.allocReg(maskOf(l, r))
	}

	f.cmpRR(l, r, left.st.typ.is64())
	f.a.SetccReg(condA, result)
	f.a.AluRI(3, result, 0, false) // sbb result, 0; CF still comes from CMP.

	f.pinned = f.pinned.remove(l).remove(r)
	if dest != regNone && !destWasPinned {
		f.pinned = f.pinned.remove(dest)
	}
	f.release(l)
	f.release(r)
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.op = opNone
	f.stats.peep("three-way-unsigned")
	return result
}

func matchThreeWayUnsignedCompare(node *elem) (left, right *elem, ok bool) {
	if node == nil || node.kind != ekDeferred || node.op != opSub || node.typ != mtI32 ||
		node.arg0 == nil || node.arg1 == nil || node.arg0.kind != ekDeferred || node.arg1.kind != ekDeferred {
		return nil, nil, false
	}
	a, b := node.arg0, node.arg1
	switch {
	case a.op == opGtU && b.op == opLtU:
		left, right = a.arg0, a.arg1
	case a.op == opLtU && b.op == opGtU:
		// (a < b) - (a > b) is the ordinary three-way comparison of b to a.
		left, right = a.arg1, a.arg0
	default:
		return nil, nil, false
	}
	if !sameSimpleThreeWayValue(a.arg0, b.arg0) || !sameSimpleThreeWayValue(a.arg1, b.arg1) {
		return nil, nil, false
	}
	return left, right, true
}

func sameSimpleThreeWayValue(a, b *elem) bool {
	if a == nil || b == nil || a.kind != ekValue || b.kind != ekValue || a.st.kind != b.st.kind || a.st.typ != b.st.typ {
		return false
	}
	switch a.st.kind {
	case stConst:
		return a.st.cval == b.st.cval
	case stLocalRef, stLocalReg, stGlobalRef, stGlobReg:
		return a.st.idx == b.st.idx
	default:
		return false
	}
}
