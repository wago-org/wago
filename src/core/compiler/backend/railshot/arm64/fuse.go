//go:build arm64

package arm64

// Compare→branch fusion: when a relational compare (or eqz) feeds directly into
// br_if or if, emit the compare's CMP and branch on its NZCV flags, skipping the
// Cset + materialize + CMP that a standalone boolean would need. This is the
// single most impactful peephole for loops (the loop-condition test each
// iteration). Ported from WARP's flag-forwarding / backend/railshot/amd64's fused cmp.

// invertCond returns the condition that holds exactly when c does not. AArch64
// condition codes (like x86) are paired by their low bit.
func invertCond(c Cond) Cond { return c ^ 1 }

// isFusableCompare reports whether e is a deferred relational/eqz node whose flag
// result can be branched on directly.
func isFusableCompare(e *elem) bool {
	return e != nil && e.kind == ekDeferred && (isCompare(e.op) || e.op == opEqz)
}

// flushBelow materializes every operand strictly below node's valent block into
// its canonical frame slots (v128 values use two adjacent slots), leaving node's
// block on top untouched. Returns the number of flushed operands. Used before emitting a
// fused compare so the subsequent branch's moveSlots reads canonical slots and no
// flag-clobbering flush happens between the CMP and the B.cond.
func (f *fn) flushBelow(node *elem) int {
	f.stats.addFlushBelow()
	f.invalidateGlobalsCache() // a following call would clobber the cached cell-ptr register
	f.invalidateBoundsCert()   // bounds facts are valid only within a straight-line region
	base := baseOfValentBlock(node)
	var below []*elem
	for cur := base.prev; cur != f.s.head; cur = baseOfValentBlock(cur).prev {
		below = append(below, cur)
	}
	for i, j := 0, len(below)-1; i < j; i, j = i+1, j-1 {
		below[i], below[j] = below[j], below[i]
	}
	slot := 0
	for _, root := range below {
		typ := rootMachineType(root)
		f.stats.addFlushBelowRoot(root.kind == ekDeferred)
		if root.kind == ekValue && root.st.kind == stSlot && root.st.slot == slot && root.st.typ == typ {
			slot += typ.stackSlots()
			continue
		}
		if typ == mtV128 {
			x := f.materializeV128(root)
			f.a.StrQ(SP, f.spillOff(slot), x)
			f.releaseF(x)
			root.kind = ekValue
			f.replaceStorage(root, storage{kind: stSlot, typ: mtV128, slot: slot})
			slot += 2
			continue
		}
		if root.kind == ekValue && (root.st.kind == stLocalReg || root.st.kind == stGlobReg) {
			if root.st.typ.isFloat() {
				f.a.StrD(SP, f.spillOff(slot), root.st.reg)
			} else {
				f.st64(SP, f.spillOff(slot), root.st.reg)
			}
			f.replaceStorage(root, storage{kind: stSlot, typ: typ, slot: slot})
			slot++
			continue
		}
		if root.kind == ekValue && typ.isFloat() {
			x := f.materializeF(root)
			f.a.StrD(SP, f.spillOff(slot), x)
			f.releaseF(x)
			root.kind = ekValue
			f.replaceStorage(root, storage{kind: stSlot, typ: typ, slot: slot})
			slot++
			continue
		}
		r := f.materialize(root)
		f.st64(SP, f.spillOff(slot), r)
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

// condenseToFlags emits a relational/eqz node's CMP (no Cset), consumes the node
// and its operands, and returns the condition code that is true when the
// comparison holds. The CMP must be the last flag-affecting instruction before
// the branch, so callers flush everything below first.
func (f *fn) condenseToFlags(node *elem) Cond {
	f.stats.peep("cmp-branch-fuse")
	// eqz over a fusable compare fuses by INVERTING the branch condition rather than
	// materializing the inner boolean (the Cset+CMP an `eqz(a<b)` otherwise
	// costs): `eqz(a<b)` branches on !(a<b) directly. Nested eqz peels too
	// (`eqz(eqz(x))` → x, double inversion). Each peel just unlinks the wrapper elem
	// (its operand sits directly below it); the inner node's CMP is still emitted
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
	applyInvert := func(cc Cond) Cond {
		if invert {
			return invertCond(cc)
		}
		return cc
	}
	w := node.typ.is64()
	if node.op == opEqz {
		// CMP #0 does not write its operand, so a register-resident value (a pinned
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
		// eqz → CMP L, #0 (SUBS XZR,L,#0); condE (CondEQ) holds when L == 0. A
		// branch consumer could equivalently use CBZ/CBNZ, but the compare keeps
		// parity with the relational path so the caller's B.cond is uniform.
		if w {
			f.a.CmpImm64(L, 0)
		} else {
			f.a.CmpImm32(L, 0)
		}
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
		// AArch64 CMP takes a 12-bit unsigned immediate; anything outside [0,4095]
		// (including every negative comparand) falls back to materializing the
		// constant and comparing register-register.
		if v := right.st.cval; uint64(v) <= 0xFFF {
			if w {
				f.a.CmpImm64(L, uint32(v))
			} else {
				f.a.CmpImm32(L, uint32(v))
			}
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
		// arm64 has no memory operand: LDR the spilled value, then compare reg-reg.
		t := f.allocReg(maskOf(L))
		f.ld64(t, SP, f.spillOff(right.st.slot))
		f.cmpRR(L, t, w)
		f.release(t)
	case stLocalRef:
		// arm64 has no memory operand: LDR the local from its frame slot, then compare.
		t := f.allocReg(maskOf(L))
		f.ld64(t, SP, f.localOff(right.st.idx))
		f.cmpRR(L, t, w)
		f.release(t)
	case stMemRef:
		// A deferred linear-memory load can NEVER be folded as a CMP operand on
		// arm64 (memRefFoldable is always false, §4a), so we always materialize the
		// value into a register first, then compare register-register.
		r := f.memRefValue(right.st)
		f.cmpRR(L, r, w)
		f.release(r)
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

// brIfFused lowers `<compare> br_if L` as CMP + conditional branch.
func (f *fn) brIfFused(top *elem, labelIdx uint32) error {
	return f.brIfFusedSet(top, labelIdx, regNone)
}

// brIfFusedSet is brIfFused with an optional `local.tee` destination. CSET is
// flag-transparent, so storing the compare result between CMP and B.cond keeps
// the flags live and avoids rematerializing/re-comparing the boolean.
func (f *fn) brIfFusedSet(top *elem, labelIdx uint32, setDst Reg) error {
	fi := len(f.ctrl) - 1 - int(labelIdx)
	if fi < 0 {
		return errBadLabel
	}
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr) // before the compare: loads/stores stay clear of the flags window
	k := f.flushBelow(top)
	cc := f.condenseToFlags(top)
	if setDst != regNone {
		f.a.Cset32(setDst, cc)
	}
	a := fr.branchN
	// Emit the edge and measure it. The edge helpers emit only LDR/STR/MOV, which
	// are position-independent AND leave NZCV untouched — so the compare's flags
	// stay live across them and the bytes can be relocated below.
	mark := f.a.Len()
	f.storeLoopPinsLeaving(fi)
	if fr.regMerge1 {
		f.branchEdgeToMerge1(fr, k)
	} else {
		f.moveBranchValues(fr, k, a)
	}
	if f.a.Len() == mark {
		// Empty edge: branch straight to the target when the compare holds — one
		// instruction, no skip branch, no padding NOP in the loop body.
		if branchFoldEnabled && f.condBranchJump(fr, cc) {
			return nil
		}
		over := f.a.Bcond(invertCond(cc))
		f.branchJump(fr)
		f.a.PatchBranch19(over, f.a.Len())
		return nil
	}
	// Non-empty edge: insert the skip guard right after the CMP (keeping the flag
	// window tight) by relocating the edge bytes up one word.
	f.edgeScratch = append(f.edgeScratch[:0], f.a.B[mark:]...)
	f.a.B = f.a.B[:mark]
	over := f.a.Bcond(invertCond(cc)) // fall through when the compare is false
	f.a.B = append(f.a.B, f.edgeScratch...)
	f.branchJump(fr)
	f.a.PatchBranch19(over, f.a.Len())
	return nil
}
