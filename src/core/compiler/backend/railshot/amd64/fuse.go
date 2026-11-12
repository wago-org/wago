// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"math/bits"
	"os"
	"runtime"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

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
	return e != nil && e.isDeferred() && (isCompare(e.deferredOp()) || isFloatCompare(e.deferredOp()) || e.deferredOp() == opEqz)
}

var zeroCompareEnabled = runtime.GOOS == "linux" && os.Getenv("WAGO_AMD64_NO_ZERO_COMPARE") != "1"

// XOR and modular subtraction are zero exactly when their inputs are equal.
// This applies only when the result is consumed as a truth value.
func isFusableBranchPredicate(e *elem) bool {
	return isFusableCompare(e) || zeroCompareEnabled && e != nil && e.isDeferred() && (e.deferredOp() == opXor || e.deferredOp() == opSub)
}

// Small i32 mask conditions can use a short byte TEST for select's zero flag.
func isMaskedI32Condition(node *elem) bool {
	if !swarMaskTestEnabled || node == nil || !node.isDeferred() || node.deferredOp() != opAnd ||
		node.valueType() != mtI32 || node.arg1 == nil || !node.arg1.isValue() || node.arg1.st.kind != stConst {
		return false
	}
	mask := uint32(node.arg1.st.cval)
	return mask != 0 && mask <= 0xff
}

// tryMaskedPredicateToFlags lowers a nonzero i32 mask condition or an eqz
// around either integer width's mask. TEST avoids a temporary AND result.
// The helper consumes the operands but leaves the outer node for its consumer.
func (f *fn) tryMaskedPredicateToFlags(node *elem) (Cond, bool) {
	if !swarMaskTestEnabled || node == nil {
		return 0, false
	}
	nonzero := isMaskedI32Condition(node)
	if !nonzero && node.deferredOp() != opEqz {
		return 0, false
	}
	inner := node
	if !nonzero {
		inner = node.arg0
	}
	if inner == nil || !inner.isDeferred() || inner.deferredOp() != opAnd ||
		inner.arg1 == nil || !inner.arg1.isValue() || inner.arg1.st.kind != stConst ||
		inner.arg1.st.cval == 0 {
		return 0, false
	}

	left := inner.arg0
	var x Reg
	owned := false
	switch {
	case left.isValue() && (left.st.kind == stLocalReg || left.st.kind == stGlobReg):
		x = left.st.reg
	case left.isValue() && left.st.kind == stReg:
		x, owned = left.st.reg, true
	default:
		x, owned = f.materialize(left), true
	}
	f.pinned = f.pinned.add(x)
	w := inner.valueType().is64()
	c := inner.arg1.st.cval
	mask := uint64(c)
	if !w {
		mask = uint64(uint32(c))
	}
	cc := condE
	if singleBitMaskTestEnabled && f.policy.CompactNative && mask&(mask-1) == 0 {
		f.a.BtImm(x, uint8(bits.TrailingZeros64(mask)), w)
		cc = condAE // BT copies the selected bit to CF; AE means CF=0.
		f.stats.peep("single-bit-mask-test")
	} else if nonzero {
		// Admission proves every significant mask bit is in this byte. The
		// consumer needs only ZF; byte TEST's sign flag is deliberately unused.
		f.a.TestImm8(x, byte(mask))
		f.stats.peep("select-mask-byte-test")
	} else if !w || fitsImm32(c) {
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
	if nonzero {
		cc = invertCond(cc)
	}
	return cc, true
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
	belowSlots := 0
	for cur := base.prev; cur != f.s.head; cur = baseOfValentBlock(cur).prev {
		below = append(below, cur)
		belowSlots += rootMachineType(cur).stackSlots()
	}
	f.tmpBelow = below
	f.tmpBelowWritten = max(f.tmpBelowWritten, len(below))
	for i, j := 0, len(below)-1; i < j; i, j = i+1, j-1 {
		below[i], below[j] = below[j], below[i]
	}
	if regallocCheckEnabled {
		f.checkBeginFlush(below)
		defer f.checkEndFlush()
	}
	// Canonical stores must not overwrite any live prefix, condition or argument
	// source. New allocator spills during materialization need the same floor.
	oldFloor := f.spillFloor
	f.spillFloor = max(oldFloor, belowSlots)
	f.stageFlushSpills(belowSlots, below)

	slot := 0
	for _, root := range below {
		typ := rootMachineType(root)
		if root.isValue() && root.st.kind == stSlot && root.st.slotIndex() == slot && root.st.typ == typ {
			slot += typ.stackSlots()
			continue
		}
		if typ == mtV128 {
			x := f.materializeV128(root)
			f.mov128StoreDisp(RSP, f.spillOff(slot), x)
			f.releaseF(x)
			root.setElemKind(ekValue)
			f.replaceStorage(root, storage{kind: stSlot, typ: mtV128, slot: uint32(slot)})
			slot += 2
			continue
		}
		if root.isValue() && (root.st.kind == stLocalReg || root.st.kind == stGlobReg) {
			if root.st.typ.isFloat() {
				f.a.FStoreDisp(RSP, f.spillOff(slot), root.st.reg, true)
			} else {
				f.a.Store64(RSP, f.spillOff(slot), root.st.reg)
			}
			f.replaceStorage(root, storage{kind: stSlot, typ: typ, slot: uint32(slot)})
			slot++
			continue
		}
		if root.isValue() && typ.isFloat() {
			x := f.materializeF(root)
			f.a.FStoreDisp(RSP, f.spillOff(slot), x, true)
			f.releaseF(x)
			root.setElemKind(ekValue)
			f.replaceStorage(root, storage{kind: stSlot, typ: typ, slot: uint32(slot)})
			slot++
			continue
		}
		r := f.materialize(root)
		f.a.Store64(RSP, f.spillOff(slot), r)
		f.release(r)
		root.setElemKind(ekValue)
		f.replaceStorage(root, storage{kind: stSlot, typ: typ, slot: uint32(slot)})
		slot++
	}
	if slot > f.maxSpill {
		f.maxSpill = slot
	}
	f.spillFloor = oldFloor
	return len(below)
}

// condenseToFlags emits a comparison or masked predicate's CMP/TEST, consumes the
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
	if f.opt(optSTFlags) {
		for node.deferredOp() == opEqz && isFusableBranchPredicate(node.arg0) {
			inner := node.arg0
			f.erase(node) // drop the eqz wrapper; `inner` becomes the top of the block
			f.s.exposeLogicalRoot(inner)
			f.stats.peep("eqz-fold")
			node = inner
			invert = !invert
		}
	}
	if !invert {
		if cc, ok := f.tryMaskedPredicateToFlags(node); ok {
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
	if isFloatCompare(node.deferredOp()) {
		cc := f.emitFloatCompareFlags(node)
		f.erase(node)
		f.stats.peep("float-branch-fuse")
		return applyInvert(cc)
	}
	if cc, ok := f.tryMemoryCompareToFlags(node); ok {
		f.erase(node)
		return applyInvert(cc)
	}
	w := node.valueType().is64()
	if node.deferredOp() == opEqz {
		// TEST does not write its operand, so a register-resident value (a pinned
		// local — e.g. a loop counter — or an owned temp) is tested in place with no
		// copy, mirroring the relational path below.
		a := node.arg0
		var L Reg
		ownedL := false
		switch {
		case a.isValue() && (a.st.kind == stLocalReg || a.st.kind == stGlobReg):
			L = a.st.reg
		case a.isValue() && a.st.kind == stReg:
			L, ownedL = a.st.reg, true
		default:
			L, ownedL = f.materialize(a), true
		}
		if !f.reuseInlineTailArgumentFlags(L, w) {
			f.a.TestSelf(L, w)
		}
		if ownedL {
			f.release(L)
		}
		f.consumeBlockBelow(node)
		f.erase(node)
		return applyInvert(condE)
	}
	cc := condNE
	if node.deferredOp() != opXor && node.deferredOp() != opSub {
		cc = condOf(node.deferredOp())
	} else {
		f.stats.peep("zero-compare")
	}
	cc = applyInvert(cc)
	left, right := node.arg0, node.arg1
	// Specialization often exposes a constant on the source comparison's
	// left. Commute two concrete operands so CMP can use its immediate form.
	if f.inlineTailConstants != nil && left.isValue() && left.st.kind == stConst && fitsImm32(left.st.cval) && right.isValue() &&
		(right.st.kind == stReg || right.st.kind == stLocalReg || right.st.kind == stGlobReg) {
		switch cc {
		case condB:
			cc = condA
		case condA:
			cc = condB
		case condBE:
			cc = condAE
		case condAE:
			cc = condBE
		case condL:
			cc = condG
		case condG:
			cc = condL
		case condLE:
			cc = condGE
		case condGE:
			cc = condLE
		}
		left, right = right, left
		f.stats.peep("inline-tail-compare-immediate")
	}
	// CMP does not write its left operand, so a register-resident left (an owned
	// temp or a pinned local) can be compared in place — no copy needed.
	var L Reg
	ownedL := false
	switch {
	case left.isValue() && (left.st.kind == stLocalReg || left.st.kind == stGlobReg):
		L = left.st.reg
	case left.isValue() && left.st.kind == stReg:
		L, ownedL = left.st.reg, true
	default:
		L, ownedL = f.materialize(left), true
	}
	if right.isDeferred() {
		// condense rewrites the existing operand node in place; keep that owner
		// instead of allocating a duplicate register-value node.
		f.condense(right, regNone)
		// Fixed-register operations may have spilled left. Recover its current
		// location before pinning it, retaining borrowed-register ownership.
		L, ownedL = f.materializeRead(left)
	}
	f.pinned = f.pinned.add(L)
	compareStart := f.a.Len()
	reused := f.tryReuseInlineTailCompare(L, right, w)
	switch right.st.kind {
	case stConst:
		if fitsImm32(right.st.cval) {
			if !reused {
				f.a.AluRI(cmpDigit, L, int32(right.st.cval), w)
			}
		} else {
			t := f.allocReg(maskOf(L))
			f.loadConst(t, right.st)
			f.cmpRR(L, t, w)
			f.release(t)
		}
	case stReg:
		if !reused {
			f.cmpRR(L, right.st.reg, w)
		}
		f.release(right.st.reg)
	case stLocalReg, stGlobReg:
		if !reused {
			f.cmpRR(L, right.st.reg, w)
		}
	case stSlot:
		f.a.AluRM(cmpRMcode, L, RSP, f.spillOff(right.st.slotIndex()), w)
	case stLocalRef:
		f.a.AluRM(cmpRMcode, L, RSP, f.localAddr(right.st.index()), w)
	case stMemRef:
		if memRefFoldable(right.st, w) {
			if regallocCheckEnabled {
				f.checkConsumeMemRef(right)
			}
			f.a.AluIdx(cmpRMcode, L, RBX, right.st.reg, right.st.memDisp(), w)
		} else {
			r := f.memRefValue(right)
			f.cmpRR(L, r, w)
			f.release(r)
		}
		f.releaseMemRef(right.st)
	}
	if !reused && f.inlineTailCompare.active {
		f.inlineTailCompare.lastCompare = compareStart
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
func (f *fn) brIfFused(r *wasm.Reader, top *elem, labelIdx uint32) error {
	fi := f.sourceLabelFrame(labelIdx)
	if fi < 0 {
		return errBadLabel
	}
	fr := &f.ctrl[fi]
	// A successful check before a forward branch also dominates its
	// fallthrough. Keep only local-based facts, which are invalidated when
	// their source local changes; other certificate kinds have different rules.
	var savedBounds [2]boundsCert
	if fallthroughBoundsEnabled && fr.kind == cfBlock && fi < len(f.ctrl)-1 && f.boundsCertSources != 0 {
		n := 0
		for _, c := range f.boundsCerts {
			if c.kind == 1 {
				savedBounds[n] = c
				n++
				if n == len(savedBounds) {
					break
				}
			}
		}
	}
	loopHeader := false
	counter := -1
	if f.opt(optCountedLoopLatch) && !f.interruptible && !f.usesCalls && top.deferredOp() == opEqz &&
		labelIdx == 1 && len(f.ctrl) >= 2 && fi == len(f.ctrl)-2 {
		loop := &f.ctrl[len(f.ctrl)-1]
		counter, loopHeader = localAddressKey(top.arg0)
		loopHeader = loopHeader && loop.kind == cfLoop && loop.paramN == 0 && loop.resultN == 0 &&
			fr.kind == cfBlock && fr.branchArity() == 0 && f.a.Len() == loop.controlSite
	}
	k := f.flushBelow(top)
	coldExit := f.callFreeLoopExit(fi)
	var saved localStateSnapshot
	if coldExit && len(f.locals) > len(saved) {
		coldExit = false
	}
	if !coldExit {
		f.convergeBranchLocals(fr) // before the compare: loads/stores stay clear of the flags window
	}
	if loopHeader && f.a.Len() != f.ctrl[len(f.ctrl)-1].controlSite {
		loopHeader = false
	}
	cc := f.condenseToFlags(top)
	directExit := false
	if coldExit {
		// Predicate evaluation can reclaim pins too. Snapshot the state actually
		// present on the fallthrough path, after all deferred work has run.
		saved, _ = f.snapshotLocalStates()
		directExit = f.retainCallFreeLoopExitRegisters(fi)
	} else if f.pinRelinquished {
		// The compare can home a pin after the initial convergence. All lazy
		// zeros were materialized above, so this repair emits only flag-preserving
		// loads and stores before the conditional jump.
		f.convergeBranchLocals(fr)
	}
	if f.tryScalarPosttestLoop(fr, fi, k, cc) {
		return nil
	}
	a := fr.branchArity()
	if directExit && k == 0 && a == 0 && !loopHeader {
		// Every pin already satisfies the register-only target. No reconciliation,
		// payload move or handler restore can execute on this guarded void edge.
		f.frameAddEnd(fr, f.a.JccPlaceholder(cc))
		fr.set(ctrlEndReachable, true)
		f.restoreLocalStates(saved)
		f.stats.peep("callfree-loop-exit-cold")
		f.stats.peep("callfree-loop-direct-exit")
		for _, c := range savedBounds {
			if c.kind != 0 {
				f.boundsCertUpdate(c.kind, c.idx, c.extent)
			}
		}
		return nil
	}
	over := f.a.JccPlaceholder(invertCond(cc)) // fall through when the compare is false
	if coldExit {
		// MOV loads/stores used by local reconciliation preserve x86 flags, so
		// the taken exit alone pays this work after the fused compare.
		f.convergeBranchLocals(fr)
	}
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, k)
	} else {
		f.moveBranchValues(fr, k, a)
	}
	f.restoreBranchHandlers(fi)
	f.branchJump(fr)
	f.a.PatchRel32(over, f.a.Len())
	if coldExit {
		f.restoreLocalStates(saved)
		f.stats.peep("callfree-loop-exit-cold")
	}
	f.recordBrFold(over)
	if loopHeader {
		loop := &f.ctrl[len(f.ctrl)-1]
		_, isFloat, pinned := f.pinReg(counter)
		if pinned && !isFloat {
			f.tryHoistLinearSumBounds(r, counter, loop)
			f.ensureCtrlMerge(loop).setCountedLoop(counter, f.a.Len())
		}
	}
	for _, c := range savedBounds {
		if c.kind != 0 {
			f.boundsCertUpdate(c.kind, c.idx, c.extent)
		}
	}
	return nil
}
