// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var selectSubtractEnabled = os.Getenv("WAGO_ARM64_NO_SELECT_SUBTRACT") != "1"

func isSelectLocal(e *elem) bool {
	return e != nil && e.elemKind() == ekValue && (e.st.kind == stLocalReg || e.st.kind == stLocalRef) && !e.st.hasGCRoot()
}

func sameSelectLocal(a, b *elem) bool {
	return isSelectLocal(a) && isSelectLocal(b) && a.st.typ == b.st.typ && a.st.idx == b.st.idx
}

func reverseSelectCompare(cc Cond) Cond {
	switch cc {
	case condL:
		return condG
	case condG:
		return condL
	case condLE:
		return condGE
	case condGE:
		return condLE
	case condB:
		return condA
	case condA:
		return condB
	case condBE:
		return condAE
	case condAE:
		return condBE
	}
	return cc // EQ/NE are symmetric.
}

// Fuse a - select(0,b,compare(a,b)) before select becomes an eager sink.
// All repeated operands are stable local reads and all other operands are
// constants, so no trapping or effectful expression is reordered or skipped.
// SUBS computes both the wrapped difference and comparison flags; CSEL chooses
// the original minuend or difference. Both inputs remain read-only.
func (f *fn) trySelectSubtract(r *wasm.Reader) (bool, error) {
	if !f.opt(optSelectSubtract) {
		return false, nil
	}
	op, ok := r.Peek()
	if !ok || op != 0x6b && op != 0x7d {
		return false, nil
	}
	typ := mtI32
	if op == 0x7d {
		typ = mtI64
	}
	cond := f.s.back()
	if cond == nil || !cond.isDeferred() || cond.deferredOp() < opEq || cond.deferredOp() > opGeU || cond.st.typ != typ {
		return false, nil
	}
	b := f.s.prev(f.s.baseOfValentBlock(cond))
	if b == f.s.head {
		return false, nil
	}
	a := f.s.prev(f.s.baseOfValentBlock(b))
	if a == f.s.head {
		return false, nil
	}
	minuend := f.s.prev(f.s.baseOfValentBlock(a))
	if minuend == f.s.head {
		return false, nil
	}
	zero, other := a, b
	isZero := func(e *elem) bool {
		return e.elemKind() == ekValue && e.st.kind == stConst && e.st.typ == typ && e.st.cval == 0
	}
	zeroTrue := isZero(a)
	if !zeroTrue {
		zero, other = b, a
		if !isZero(zero) {
			return false, nil
		}
	}
	if !isSelectLocal(minuend) || !isSelectLocal(other) || minuend.st.typ != typ || other.st.typ != typ {
		return false, nil
	}
	left, right := f.s.arg0(cond), f.s.arg1(cond)
	cc := condOf(cond.deferredOp())
	if sameSelectLocal(left, minuend) && sameSelectLocal(right, other) {
	} else if sameSelectLocal(left, other) && sameSelectLocal(right, minuend) {
		cc = reverseSelectCompare(cc)
	} else {
		return false, nil
	}
	if !zeroTrue {
		cc = invertCond(cc)
	}
	// A following pinned assignment can consume the CSEL result directly.
	// Realize older reads below this complete expression before its pin changes.
	scan := *r
	if _, err := scan.Byte(); err != nil {
		return false, err
	}
	sink, tee, dest := -1, false, regNone
	if next, ok := scan.Peek(); ok && (next == 0x21 || next == 0x22) {
		candidate := scan
		_, _ = candidate.Byte()
		index, err := candidate.U32()
		if err != nil {
			return false, err
		}
		local := int(index) + f.localBase
		if local >= 0 && local < f.nLocals && f.localType[local] == typ {
			if pr, floating, pinned := f.pinReg(local); pinned && !floating {
				sink, tee, dest = local, next == 0x22, pr
				scan = candidate
				f.realizeLocalRefs(local, f.s.baseOfValentBlock(minuend))
			}
		}
	}
	*r = scan
	saved := f.pinned
	x, ownX := f.materializeRead(minuend)
	f.pinned = f.pinned.add(x)
	y, ownY := f.materializeRead(other)
	f.pinned = f.pinned.add(y)
	out := f.allocReg(0)
	if typ == mtI64 {
		f.a.Subs64(out, x, y)
	} else {
		f.a.Subs32(out, x, y)
	}
	if dest == regNone {
		dest = out
	}
	f.a.Csel(dest, x, out, cc, typ == mtI64)
	f.pinned = saved
	f.consumeBlockBelow(cond)
	f.erase(cond)
	f.erase(b)
	f.erase(a)
	f.erase(minuend)
	if ownX {
		f.release(x)
	}
	if ownY && (!ownX || x != y) {
		f.release(y)
	}
	if dest != out {
		f.release(out)
	}
	result := f.pushReg(dest, typ)
	if typ == mtI32 {
		result.st.setValueFacts(factUpper32Zero)
	}
	if sink >= 0 {
		if sink < 64 {
			f.localWritten |= uint64(1) << sink
		}
		f.setLocal(r, sink, tee)
	}
	f.stats.peep("select-subtract")
	return true, nil
}
