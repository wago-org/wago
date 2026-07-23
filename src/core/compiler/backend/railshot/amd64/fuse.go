//go:build amd64

package amd64

// Compare→branch fusion: when a relational compare (or eqz) feeds directly into
// br_if or if, emit the compare's CMP/TEST and branch on its flags, skipping the
// SETcc + materialize + TEST that a standalone boolean would need. This is the
// single most impactful peephole for loops (the loop-condition test each
// iteration). Ported from WARP's flag-forwarding / backend/railshot/amd64's fused cmp.

// invertCond returns the condition that holds exactly when c does not. x86
// condition codes are paired by their low bit.
func invertCond(c Cond) Cond { return c ^ 1 }

// isFusableCompare reports whether e is a deferred relational/eqz node whose flag
// result can be branched on directly.
func isFusableCompare(e *elem) bool {
	return e != nil && e.kind == ekDeferred && (isCompare(e.op) || e.op == opEqz)
}

// tryMaskedEqzToFlags recognizes `(x & mask) == 0`, the core reduction used by
// packed-byte/SWAR algorithms, and emits TEST directly. The ordinary lowering
// materializes x&mask and then tests that result; TEST computes identical flags
// without writing the temporary. The helper consumes the two deferred nodes but
// leaves the outer node for either a branch consumer or SETcc materialization.
func (f *fn) tryMaskedEqzToFlags(node *elem) (Cond, bool) {
	if !knownBitsEnabled || node == nil || node.op != opEqz {
		return 0, false
	}
	inner := node.arg0
	if inner == nil || inner.kind != ekDeferred || inner.op != opAnd ||
		inner.arg1 == nil || inner.arg1.kind != ekValue || inner.arg1.st.kind != stConst ||
		inner.arg1.st.cval == 0 {
		return 0, false
	}

	left := inner.arg0
	var x Reg
	owned := false
	switch {
	case left.kind == ekValue && (left.st.kind == stLocalReg || left.st.kind == stGlobReg):
		x = left.st.reg
	case left.kind == ekValue && left.st.kind == stReg:
		x, owned = left.st.reg, true
	default:
		x, owned = f.materialize(left), true
	}
	f.pinned = f.pinned.add(x)
	w := inner.typ.is64()
	c := inner.arg1.st.cval
	if !w || fitsImm32(c) {
		f.a.TestImm(x, uint32(c), w)
	} else {
		t := f.allocReg(maskOf(x))
		f.loadConst(t, inner.arg1.st)
		f.a.TestReg(x, t, true)
		f.release(t)
	}
	f.pinned = f.pinned.remove(x)
	if owned {
		f.release(x)
	}
	f.stats.peep("swar-mask-test")
	f.consumeBlockBelow(node)
	return condE, true
}

// flushBelow materializes every operand strictly below node's valent block into
// its canonical frame slots (v128 values use two adjacent slots), leaving node's
// block on top untouched. Returns the number of flushed operands. Used before emitting a
// fused compare so the subsequent branch's moveSlots reads canonical slots and no
// flag-clobbering flush happens between the CMP and the Jcc.
func (f *fn) flushBelow(node *elem) int {
	f.stats.addFlushBelow()
	f.invalidateGlobalsCache() // a following call would clobber the cached cell-ptr register
	f.invalidateBoundsCert()   // bounds facts are valid only within a straight-line region
	base := baseOfValentBlock(node)
	below := f.tmpBelow[:0]
	for cur := base.prev; cur != f.s.head; cur = baseOfValentBlock(cur).prev {
		below = append(below, cur)
	}
	f.tmpBelow = below
	for i, j := 0, len(below)-1; i < j; i, j = i+1, j-1 {
		below[i], below[j] = below[j], below[i]
	}
	slot := 0
	for _, root := range below {
		typ := rootMachineType(root)
		if root.kind == ekValue && root.st.kind == stSlot && root.st.slot == slot && root.st.typ == typ {
			slot += typ.stackSlots()
			continue
		}
		if typ == mtV128 {
			x := f.materializeV128(root)
			f.a.VMovdquStoreDisp(RSP, f.spillOff(slot), x)
			f.releaseF(x)
			root.kind = ekValue
			f.replaceStorage(root, storage{kind: stSlot, typ: mtV128, slot: slot})
			slot += 2
			continue
		}
		if root.kind == ekValue && (root.st.kind == stLocalReg || root.st.kind == stGlobReg) {
			if root.st.typ.isFloat() {
				f.a.FStoreDisp(RSP, f.spillOff(slot), root.st.reg, true)
			} else {
				f.a.Store64(RSP, f.spillOff(slot), root.st.reg)
			}
			f.replaceStorage(root, storage{kind: stSlot, typ: typ, slot: slot})
			slot++
			continue
		}
		if root.kind == ekValue && typ.isFloat() {
			x := f.materializeF(root)
			f.a.FStoreDisp(RSP, f.spillOff(slot), x, true)
			f.releaseF(x)
			root.kind = ekValue
			f.replaceStorage(root, storage{kind: stSlot, typ: typ, slot: slot})
			slot++
			continue
		}
		r := f.materialize(root)
		f.a.Store64(RSP, f.spillOff(slot), r)
		f.release(r)
		root.kind = ekValue
		f.replaceStorage(root, storage{kind: stSlot, typ: typ, slot: slot})
		slot++
	}
	if slot > f.maxSpill {
		f.maxSpill = slot
	}
	return len(below)
}

// condenseToFlags emits a relational/eqz node's CMP/TEST (no SETcc), consumes the
// node and its operands, and returns the condition code that is true when the
// comparison holds. The CMP must be the last flag-affecting instruction before
// the branch, so callers flush everything below first.
func (f *fn) condenseToFlags(node *elem) Cond {
	f.stats.peep("cmp-branch-fuse")
	// eqz over a fusable compare fuses by INVERTING the branch condition rather than
	// materializing the inner boolean (the SETcc+MOVZX+TEST an `eqz(a<b)` otherwise
	// costs): `eqz(a<b)` branches on !(a<b) directly. Nested eqz peels too
	// (`eqz(eqz(x))` → x, double inversion). Each peel just unlinks the wrapper elem
	// (its operand sits directly below it); the inner node's CMP/TEST is still emitted
	// LAST by the logic below, so flag safety is unchanged. This is the dominant
	// missed-fusion on branch-dense code (esbuild ~20k `relop;eqz;br` sites). Gated by
	// the stFlags kill switch (WAGO_NO_STFLAGS) as the A/B oracle.
	invert := false
	if stFlagsEnabled {
		for node.op == opEqz && isFusableCompare(node.arg0) {
			inner := node.arg0
			f.erase(node) // drop the eqz wrapper; `inner` becomes the top of the block
			f.stats.peep("eqz-fold")
			node = inner
			invert = !invert
		}
	}
	// Ordered float relational nodes (gt/ge/lt/le) lower to UCOMIS + a NaN-safe
	// condition instead of a materialized boolean. eq/ne are never deferred here.
	if node.typ.isFloat() {
		return f.condenseFCompareToFlags(node, invert)
	}
	if !invert {
		if cc, ok := f.tryMaskedEqzToFlags(node); ok {
			f.erase(node)
			return cc
		}
	}
	applyInvert := func(cc Cond) Cond {
		if invert {
			return invertCond(cc)
		}
		return cc
	}
	w := node.typ.is64()
	if node.op == opEqz {
		// TEST does not write its operand, so a register-resident value (a pinned
		// local — e.g. a loop counter — or an owned temp) is tested in place with no
		// copy, mirroring the relational path below.
		a := node.arg0
		var L Reg
		ownedL := false
		switch {
		case a.kind == ekValue && (a.st.kind == stLocalReg || a.st.kind == stGlobReg):
			L = a.st.reg
		case a.kind == ekValue && a.st.kind == stReg:
			L, ownedL = a.st.reg, true
		default:
			L, ownedL = f.materialize(a), true
		}
		f.a.TestSelf(L, w)
		if ownedL {
			f.release(L)
		}
		f.consumeBlockBelow(node)
		f.erase(node)
		return applyInvert(condE)
	}
	cc := applyInvert(condOf(node.op))
	// CMP does not write its left operand, so a register-resident left (an owned
	// temp or a pinned local) can be compared in place — no copy needed.
	left := node.arg0
	var L Reg
	ownedL := false
	switch {
	case left.kind == ekValue && (left.st.kind == stLocalReg || left.st.kind == stGlobReg):
		L = left.st.reg
	case left.kind == ekValue && left.st.kind == stReg:
		L, ownedL = left.st.reg, true
	default:
		L, ownedL = f.materialize(left), true
	}
	f.pinned = f.pinned.add(L)
	right := node.arg1
	if right.isDeferred() {
		rr := f.condense(right, regNone)
		right = &elem{kind: ekValue, st: storage{kind: stReg, typ: node.typ, reg: rr}}
	}
	switch right.st.kind {
	case stConst:
		if fitsImm32(right.st.cval) {
			f.a.AluRI(cmpDigit, L, int32(right.st.cval), w)
		} else {
			t := f.allocReg(maskOf(L))
			f.loadConst(t, right.st)
			f.cmpRR(L, t, w)
			f.release(t)
		}
	case stReg:
		f.cmpRR(L, right.st.reg, w)
		f.release(right.st.reg)
	case stLocalReg, stGlobReg:
		f.cmpRR(L, right.st.reg, w)
	case stSlot:
		f.a.AluRM(cmpRMcode, L, RSP, f.spillOff(right.st.slot), w)
	case stLocalRef:
		f.a.AluRM(cmpRMcode, L, RSP, f.localOff(right.st.idx), w)
	case stMemRef:
		if memRefFoldable(right.st, w) {
			f.a.AluIdx(cmpRMcode, L, RBX, right.st.reg, right.st.memDisp(), w)
		} else {
			r := f.memRefValue(right.st)
			f.cmpRR(L, r, w)
			f.release(r)
		}
		f.releaseMemRef(right.st)
	}
	f.pinned = f.pinned.remove(L)
	if ownedL {
		f.release(L)
	}
	f.consumeBlockBelow(node)
	f.erase(node)
	return cc
}

// brIfFused lowers `<compare> br_if L` as CMP + conditional jump.
func (f *fn) brIfFused(top *elem, labelIdx uint32) error {
	fi := len(f.ctrl) - 1 - int(labelIdx)
	if fi < 0 {
		return errBadLabel
	}
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr) // before the compare: loads/stores stay clear of the flags window
	k := f.flushBelow(top)
	cc := f.condenseToFlags(top)
	a := fr.branchN
	over := f.a.JccPlaceholder(invertCond(cc)) // fall through when the compare is false
	if fr.regMerge1 {
		f.branchEdgeToMerge1(fr, k)
	} else {
		f.moveBranchValues(fr, k, a)
	}
	f.branchJump(fr)
	f.a.PatchRel32(over, f.a.Len())
	f.recordBrFold(over)
	return nil
}
