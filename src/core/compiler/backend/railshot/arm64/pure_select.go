// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var pureSelectEnabled = os.Getenv("WAGO_ARM64_NO_PURE_SELECT") != "1"

type pureSelectChain struct {
	nodes [12]*elem
	leaf  *elem
	n     int
}

func pureSelectLeaf(e *elem, typ machineType) bool {
	if e == nil || e.elemKind() != ekValue || e.st.typ != typ || e.st.hasGCRoot() {
		return false
	}
	switch e.st.kind {
	case stConst, stReg, stSlot, stLocalReg, stLocalRef:
		return true
	}
	return false
}

func (f *fn) provePureSelectChain(e *elem, typ machineType) (p pureSelectChain, ok bool) {
	cost, hasMultiply := 0, false
	for e != nil && e.isDeferred() {
		if p.n == len(p.nodes) || e.st.typ != typ || e.st.hasGCRoot() ||
			!isBinALU(e.deferredOp()) || !pureSelectLeaf(f.s.arg1(e), typ) {
			return p, false
		}
		p.nodes[p.n] = e
		p.n++
		cost++
		if e.deferredOp() == opMul {
			cost += 3
			hasMultiply = true
		}
		e = f.s.arg0(e)
	}
	p.leaf = e
	// A conditional branch has to save enough dependent arithmetic to
	// repay the added control dependence. Keep a multiply and one cheap
	// operation on the CSEL path.
	return p, cost >= 8 && hasMultiply && pureSelectLeaf(e, typ)
}

func (f *fn) pureSelectResult(avoid regMask, need int) Reg {
	block := avoid.union(f.pinned).union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if !block.has(r) && f.regUser[r] == nil {
			need--
			if need == 0 {
				return r
			}
		}
	}
	return regNone
}

func (f *fn) readPureSelectLeaf(dst Reg, e *elem) {
	if regallocCheckEnabled {
		f.checkUse(e)
	}
	switch e.st.kind {
	case stConst:
		f.loadConst(dst, e.st)
	case stReg, stLocalReg:
		if e.st.typ == mtI64 {
			f.a.MovReg64(dst, e.st.reg)
		} else {
			f.a.MovReg32(dst, e.st.reg)
		}
	case stSlot:
		f.readPureSelectFrame(dst, f.spillOff(e.st.slotIndex()), e.st.typ)
	case stLocalRef:
		f.readPureSelectFrame(dst, f.localOff(e.st.index()), e.st.typ)
	}
}

func (f *fn) readPureSelectFrame(dst Reg, offset int32, typ machineType) {
	if typ == mtI64 {
		f.ld64(dst, SP, offset)
	} else {
		f.ld32(dst, SP, offset)
	}
}

func (f *fn) tryPureExpensiveSelect() bool {
	if !pureSelectEnabled || f.intervalActive != 0 {
		return false
	}
	cond := f.s.back()
	if cond == nil || rootMachineType(cond) != mtI32 {
		return false
	}
	b := f.s.prev(f.s.baseOfValentBlock(cond))
	if b == f.s.head {
		return false
	}
	a := f.s.prev(f.s.baseOfValentBlock(b))
	if a == f.s.head || a.isDeferred() == b.isDeferred() {
		return false
	}
	work, cheap := a, b
	skipOnTrue := !a.isDeferred()
	if skipOnTrue {
		work, cheap = b, a
	}
	typ := rootMachineType(work)
	if (typ != mtI32 && typ != mtI64) || !pureSelectLeaf(cheap, typ) {
		return false
	}
	chain, ok := f.provePureSelectChain(work, typ)
	if !ok || f.pureSelectResult(0, 2) == regNone {
		return false
	}

	// The condition executes on both paths and may trap. It can also
	// displace stack operands, so check the chain before emitting the split.
	c, owned := f.materializeRead(cond)
	chain, ok = f.provePureSelectChain(work, typ)
	result := f.pureSelectResult(maskOf(c), 1)
	if !ok || !pureSelectLeaf(cheap, typ) || result == regNone {
		return false
	}
	f.readPureSelectLeaf(result, cheap)
	var skip int
	if skipOnTrue {
		skip = f.a.Cbnz32(c)
	} else {
		skip = f.a.Cbz32(c)
	}
	f.readPureSelectLeaf(result, chain.leaf)
	wide := typ == mtI64
	for i := chain.n - 1; i >= 0; i-- {
		node := chain.nodes[i]
		right := f.s.arg1(node)
		op := node.deferredOp()
		if regallocCheckEnabled {
			f.checkUse(right)
		}
		if right.st.kind == stConst && f.aluImm(op, result, right.st.cval, wide) {
			continue
		}
		r := right.st.reg
		if right.st.kind != stReg && right.st.kind != stLocalReg {
			f.readPureSelectLeaf(X16, right)
			r = X16
		}
		f.aluRR(op, result, r, wide)
	}
	f.patchBranch19(skip, f.a.Len())
	f.shiftedALUShortEnd = 0

	// The branch changes only the result register. Release stack owners
	// once both paths join, without emitting conditional allocator work.
	for _, leaf := range [...]*elem{cheap, chain.leaf} {
		if leaf.st.kind == stReg {
			f.release(leaf.st.reg)
		}
	}
	for _, node := range chain.nodes[:chain.n] {
		if right := f.s.arg1(node); right.st.kind == stReg {
			f.release(right.st.reg)
		}
	}
	if owned {
		f.release(c)
	}
	f.consumeBlockBelow(work)
	f.erase(cond)
	f.erase(cheap)
	f.occupy(work, result)
	f.stats.peep("pure-select-branch")
	return true
}
