// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var compareAndChainEnabled = os.Getenv("WAGO_ARM64_NO_COMPARE_AND_CHAIN") != "1"

const maxCompareAndLeaves = 16

type compareAndPlan struct {
	leaves [maxCompareAndLeaves]*elem
	n      int
}

func compareAndOperand(e *elem) bool {
	if e == nil || e.elemKind() != ekValue || e.st.typ.isFloat() || e.st.typ.isV128() {
		return false
	}
	switch e.st.kind {
	case stConst, stReg, stSlot, stLocalRef, stLocalReg, stGlobReg:
		return true
	default:
		return false
	}
}

func (p *compareAndPlan) collect(s *stack, e *elem) bool {
	if e == nil || p.n == len(p.leaves) {
		return false
	}
	if e.isDeferred() && e.deferredOp() == opAnd && e.st.typ == mtI32 {
		return p.collect(s, s.arg0(e)) && p.collect(s, s.arg1(e))
	}
	if !e.isDeferred() || !isCompare(e.deferredOp()) ||
		!compareAndOperand(s.arg0(e)) || !compareAndOperand(s.arg1(e)) {
		return false
	}
	p.leaves[p.n] = e
	p.n++
	return true
}

// falseNZCV returns flags for which c is false. A conditional compare installs
// these flags when the preceding comparison failed, making the final condition
// represent the conjunction accumulated so far.
func falseNZCV(c Cond) uint8 {
	switch c {
	case condE, condAE, condA, condLE:
		return 0
	case condNE:
		return 4 // Z=1
	case condB:
		return 2 // C=1
	case condBE:
		return 2 // C=1, Z=0
	case condL:
		return 0 // N=V
	case condGE:
		return 8 // N!=V
	case condG:
		return 4 // Z=1
	default:
		panic("arm64: unsupported compare condition")
	}
}

func (f *fn) compareAndRead(e *elem, avoid regMask) (Reg, bool) {
	if e.st.kind == stConst {
		st := e.st
		if st.typ == mtI32 {
			st.cval = int64(int32(st.cval))
		}
		return f.intConstReadReg(st, avoid)
	}
	return f.materializeRead(e)
}

func (f *fn) emitCompareAndLeaf(node *elem, gate Cond, conditional bool) Cond {
	left, right := f.s.arg0(node), f.s.arg1(node)
	wide := node.st.typ.is64()
	cc := condOf(node.deferredOp())

	// Keep the ordinary comparison's immediate form for the first leaf. Later
	// leaves use CCMP's five-bit immediate when possible and otherwise obtain a
	// temporary constant register through the existing constant loader.
	if right.st.kind == stConst && (!conditional || right.st.cval >= 0 && right.st.cval <= 31) {
		l, ownL := f.compareAndRead(left, f.pinned)
		f.pinned = f.pinned.add(l)
		if conditional {
			f.a.CcmpImm(l, uint8(right.st.cval), falseNZCV(cc), gate, !wide)
		} else {
			f.cmpConst(l, right.st, wide)
		}
		f.pinned = f.pinned.remove(l)
		if ownL {
			f.release(l)
		}
		return cc
	}

	saved := f.pinned
	r, ownR := f.compareAndRead(right, f.pinned)
	f.pinned = f.pinned.add(r)
	l, ownL := f.compareAndRead(left, f.pinned)
	f.pinned = f.pinned.add(l)
	if conditional {
		f.a.CcmpReg(l, r, falseNZCV(cc), gate, !wide)
	} else {
		f.cmpRR(l, r, wide)
	}
	f.pinned = saved
	if ownL {
		f.release(l)
	}
	if ownR && r != l {
		f.release(r)
	}
	return cc
}

// tryCompareAndChain lowers a bounded tree of boolean comparison conjunctions
// to CMP; CCMP...; CSET. Every admitted operand is already a concrete,
// nontrapping integer value, so the cover changes neither evaluation effects nor
// trap order. The fixed plan keeps compilation work and memory bounded.
func (f *fn) tryCompareAndChain(node *elem, dest Reg) Reg {
	if !compareAndChainEnabled || !f.opt(optGuardedTestCCMP) || node.st.typ != mtI32 || node.deferredDepth() < 2 {
		return regNone
	}
	var p compareAndPlan
	if !p.collect(f.s, node) || p.n < 2 {
		return regNone
	}
	cc := f.emitCompareAndLeaf(p.leaves[0], 0, false)
	for _, leaf := range p.leaves[1:p.n] {
		cc = f.emitCompareAndLeaf(leaf, cc, true)
	}
	result := dest
	if result == regNone {
		result = f.allocReg(0)
	}
	f.a.Cset32(result, cc)
	f.stats.peep("compare-and-chain")
	f.stats.peep("compare-setcc")
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	return result
}
