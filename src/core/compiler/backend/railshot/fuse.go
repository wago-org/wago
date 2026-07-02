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

// flushBelow materializes every operand strictly below node's valent block into
// its canonical frame slot (position i → spillOff(i)), leaving node's block on
// top untouched. Returns the number of flushed operands. Used before emitting a
// fused compare so the subsequent branch's moveSlots reads canonical slots and no
// flag-clobbering flush happens between the CMP and the Jcc.
func (f *fn) flushBelow(node *elem) int {
	base := baseOfValentBlock(node)
	var below []*elem
	for cur := base.prev; cur != f.s.head; cur = baseOfValentBlock(cur).prev {
		below = append(below, cur)
	}
	for i, j := 0, len(below)-1; i < j; i, j = i+1, j-1 {
		below[i], below[j] = below[j], below[i]
	}
	for i, root := range below {
		if root.kind == ekValue && root.st.kind == stSlot && root.st.slot == i {
			continue
		}
		if root.kind == ekValue && root.st.kind == stLocalReg {
			if root.st.typ.isFloat() {
				f.a.FStoreDisp(RSP, f.spillOff(i), root.st.reg, true)
			} else {
				f.a.Store64(RSP, f.spillOff(i), root.st.reg)
			}
			f.replaceStorage(root, storage{kind: stSlot, typ: mtI64, slot: i})
			continue
		}
		if root.kind == ekValue && root.st.typ.isFloat() {
			x := f.materializeF(root)
			f.a.FStoreDisp(RSP, f.spillOff(i), x, true)
			f.releaseF(x)
			root.kind = ekValue
			f.replaceStorage(root, storage{kind: stSlot, typ: mtI64, slot: i})
			continue
		}
		r := f.materialize(root)
		f.a.Store64(RSP, f.spillOff(i), r)
		f.release(r)
		root.kind = ekValue
		f.replaceStorage(root, storage{kind: stSlot, typ: mtI64, slot: i})
	}
	if len(below) > f.maxSpill {
		f.maxSpill = len(below)
	}
	return len(below)
}

// condenseToFlags emits a relational/eqz node's CMP/TEST (no SETcc), consumes the
// node and its operands, and returns the condition code that is true when the
// comparison holds. The CMP must be the last flag-affecting instruction before
// the branch, so callers flush everything below first.
func (f *fn) condenseToFlags(node *elem) Cond {
	w := node.typ.is64()
	if node.op == opEqz {
		L := f.materialize(node.arg0)
		f.a.TestSelf(L, w)
		f.release(L)
		f.consumeBlockBelow(node)
		f.erase(node)
		return condE
	}
	cc := condOf(node.op)
	// CMP does not write its left operand, so a register-resident left (an owned
	// temp or a pinned local) can be compared in place — no copy needed.
	left := node.arg0
	var L Reg
	ownedL := false
	switch {
	case left.kind == ekValue && left.st.kind == stLocalReg:
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
	case stLocalReg:
		f.cmpRR(L, right.st.reg, w)
	case stSlot:
		f.a.AluRM(cmpRMcode, L, RSP, f.spillOff(right.st.slot), w)
	case stLocalRef:
		f.a.AluRM(cmpRMcode, L, RSP, f.localOff(right.st.idx), w)
	case stMemRef:
		if memRefFoldable(right.st, w) {
			f.a.AluIdx(cmpRMcode, L, RBX, right.st.reg, right.st.memDisp(), w)
		} else {
			f.loadMemRef(right.st.reg, right.st)
			f.cmpRR(L, right.st.reg, w)
		}
		f.release(right.st.reg)
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
	k := f.flushBelow(top)
	cc := f.condenseToFlags(top)
	fi := len(f.ctrl) - 1 - int(labelIdx)
	if fi < 0 {
		return errBadLabel
	}
	fr := &f.ctrl[fi]
	a, base := fr.branchN, fr.height
	over := f.a.JccPlaceholder(invertCond(cc)) // fall through when the compare is false
	if fr.regMerge1 {
		f.branchEdgeToMerge1(fr, k)
	} else {
		f.moveSlots(k-a, base, a)
	}
	f.branchJump(fr)
	f.a.PatchRel32(over, f.a.Len())
	return nil
}
