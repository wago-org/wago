// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "os"

var divRemConstPairEnabled = os.Getenv("WAGO_AMD64_DIVREM_CONST_PAIR") != "0"

// Share one quotient between adjacent deferred unsigned i32 div/rem operations.
// Each operand must be the same unchanged local and nonzero constant. Up to
// eight concrete integer constants/local reads may separate the two operand
// packets. These leaves have no effects, and local writes materialize earlier
// reads before changing their homes. Deferred work retains the ordinary path.
// In guard mode pushBinOp materializes each division before the next operation
// is parsed. The future remainder is then absent, so the ordinary path remains.
func (f *fn) tryDivRemConstPair(node *elem, dest Reg) (Reg, bool) {
	if !divRemConstPairEnabled || node.deferredOp() != opDivU || node.valueType() != mtI32 ||
		len(f.intervalReg) != 0 || f.vectorRegion.enabled {
		return regNone, false
	}
	local := node.arg0
	constant := node.arg1
	if !local.isValue() || (local.st.kind != stLocalRef && local.st.kind != stLocalReg) ||
		!constant.isValue() || constant.st.kind != stConst {
		return regNone, false
	}
	d := uint32(constant.st.cval)
	if !strengthReducibleWithMagic(int64(d), false, false, f.opt(optMagicDiv)) {
		return regNone, false
	}
	otherLocal := node.next
	for skipped := 0; skipped < 8; skipped++ {
		if otherLocal == nil || otherLocal == f.s.head || !otherLocal.isValue() {
			return regNone, false
		}
		if otherLocal.valueType() == mtI32 && otherLocal.st.kind == local.st.kind && otherLocal.st.idx == local.st.idx {
			break
		}
		if otherLocal.valueType() != mtI32 && otherLocal.valueType() != mtI64 {
			return regNone, false
		}
		switch otherLocal.st.kind {
		case stConst, stLocalRef, stLocalReg:
		default:
			return regNone, false
		}
		otherLocal = otherLocal.next
	}
	if otherLocal == nil || !otherLocal.isValue() || otherLocal.valueType() != mtI32 ||
		otherLocal.st.kind != local.st.kind || otherLocal.st.idx != local.st.idx {
		return regNone, false
	}
	otherConstant := otherLocal.next
	if otherConstant == nil || !otherConstant.isValue() || otherConstant.valueType() != mtI32 ||
		otherConstant.st.kind != stConst || uint32(otherConstant.st.cval) != d {
		return regNone, false
	}
	remainder := otherConstant.next
	if remainder == nil || !remainder.isDeferred() || remainder.valueType() != mtI32 ||
		remainder.deferredOp() != opRemU || remainder.arg0 != otherLocal || remainder.arg1 != otherConstant {
		return regNone, false
	}
	if regallocCheckEnabled {
		f.checkInputs(remainder)
	}
	// Keep the original dividend and quotient outside MUL's fixed registers.
	// divConstUnsigned's magicMulHigh spills any live RAX/RDX occupants before MUL.
	n := f.allocReg(maskOf(RAX, RDX, dest))
	f.pinned = f.pinned.add(n)
	f.condenseInto(local, n)
	q := f.allocReg(maskOf(RAX, RDX, n))
	f.pinned = f.pinned.add(q)
	f.a.MovRegReg32(q, n)
	f.divConstUnsigned(q, uint64(d), false, false)
	previous := profileOrigin{}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		previous = f.enterProfileNode(remainder)
	}
	f.remFromQuot(n, q, int64(d), false)
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		f.switchProfileOrigin(previous)
	}
	f.pinned = f.pinned.remove(q).remove(n)

	result := q
	if dest != regNone && dest != q {
		f.moveInt(dest, q, mtI32)
		f.release(q)
		result = dest
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.setDeferredOp(opNone)
	f.consumeBlockBelow(remainder)
	f.occupy(remainder, n)
	remainder.setDeferredOp(opNone)
	f.stats.peep("divrem-const-pair")
	return result, true
}
