// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"os"
)

var shiftedLinearTreeEnabled = os.Getenv("WAGO_ARM64_NO_SHIFTED_LINEAR_TREE") != "1"

// Bound a conservative scalar alternative: additions and nested shifts are
// free, assuming existing MADD and shifted-register covers consume them. Only
// simple local-times-constant products contribute a cost. This avoids charging
// for operations that the ordinary emitter can already eliminate.
func (f *fn) shiftedLinearScalarCost(e *elem, budget *int) (int, bool) {
	if e == nil || *budget == 0 {
		return 0, false
	}
	*budget--
	if e.elemKind() == ekValue {
		return 0, e.st.kind == stConst || e.st.kind == stLocalReg || e.st.kind == stLocalRef
	}
	if !e.isDeferred() {
		return 0, false
	}
	a, b := f.s.arg0(e), f.s.arg1(e)
	switch e.deferredOp() {
	case opAdd, opSub:
		x, ok := f.shiftedLinearScalarCost(a, budget)
		if !ok {
			return 0, false
		}
		y, ok := f.shiftedLinearScalarCost(b, budget)
		return x + y, ok
	case opShl:
		if b == nil || b.elemKind() != ekValue || b.st.kind != stConst {
			return 0, false
		}
		return f.shiftedLinearScalarCost(a, budget)
	case opMul:
		if a != nil && a.elemKind() == ekValue && a.st.kind == stConst {
			a, b = b, a
		}
		if a == nil || a.elemKind() != ekValue || (a.st.kind != stLocalReg && a.st.kind != stLocalRef) || b == nil || b.elemKind() != ekValue || b.st.kind != stConst {
			return 0, false
		}
		c := uint32(b.st.cval)
		if c == 0 || c == 1 {
			return 0, true
		}
		if c&(c-1) == 0 {
			return 1, true
		}
		if _, _, ok := f.selectShiftedIntegerMultiply(b.st, false); ok {
			return 1, true
		}
		if _, ok := f.cachedIntConst(b.st); ok {
			return 1, true
		}
		moves := intConstWideMoveCost(b.st.cval, 1)
		if a64.LogicalImmediate32(c) {
			moves = 1
		}
		return 1 + moves, true
	}
	return 0, false
}

func (f *fn) profitableShiftedLinearTree(node *elem, tree *linearIntegerTree) bool {
	shift := f.s.arg1(node)
	if shift == nil || shift.elemKind() != ekValue || shift.st.kind != stConst || uint32(shift.st.cval)&31 == 0 {
		return false
	}
	budget := 64
	scalar, ok := f.shiftedLinearScalarCost(node, &budget)
	if !ok {
		return false
	}
	// Count the root shift as free too: an ADD parent may absorb it.
	selected := 0
	if tree.constant != 0 {
		selected = intConstWideMoveCost(int64(int32(tree.constant)), 1)
	}
	for _, t := range tree.terms[:tree.count] {
		c := t.coefficient
		if c == 0 {
			continue
		}
		selected++
		if c != 1 && c != ^uint32(0) && c&(c-1) != 0 {
			selected += intConstWideMoveCost(int64(int32(c)), 1)
		}
	}
	return scalar >= selected+2
}
