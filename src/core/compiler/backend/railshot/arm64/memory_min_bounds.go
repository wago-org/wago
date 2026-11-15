// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

var memoryMinimumBoundsEnabled = true

func (f *fn) minimumMemoryAccess(memoryIndex uint32, offset uint64, size int) bool {
	if !f.opt(optMemoryMinimumBounds) {
		return false
	}
	mt, ok := f.memoryType(memoryIndex)
	if !ok || !shared.ProvenMinimumAccess(mt, 0, offset, size) {
		return false
	}
	budget := 32
	maximum := f.unsignedAddressMaximum(f.s.back(), mt.Limits.Addr64, &budget)
	return shared.ProvenMinimumAccess(mt, maximum, offset, size)
}

// The transient expression tree needs no new facts or per-local storage. A
// fixed node budget bounds recursive work; unknown expressions use the full
// unsigned domain. Address lowering still evaluates every source operation.
func (f *fn) unsignedAddressMaximum(e *elem, wide bool, budget *int) uint64 {
	limit := uint64(^uint32(0))
	typ := mtI32
	if wide {
		limit = ^uint64(0)
		typ = mtI64
	}
	if e == nil || *budget <= 0 || e.st.typ != typ {
		return limit
	}
	*budget--
	if e.elemKind() == ekValue {
		if e.st.kind == stConst {
			value := uint64(e.st.cval)
			if !wide {
				value = uint64(uint32(value))
			}
			return value
		}
		if !wide {
			if maximum, ok := f.localAddressMaximum(e); ok {
				return maximum
			}
		}
		return limit
	}
	if e.elemKind() != ekDeferred {
		return limit
	}
	op := e.deferredOp()
	switch op {
	case opAdd, opAnd, opOr, opXor, opMul, opShl, opShrU:
	default:
		return limit
	}
	left, right := f.s.arg0(e), f.s.arg1(e)
	// A constant-entry root loop may deliberately use a high unsigned counter
	// which wraps only in the address expression (for example, a negative
	// displacement counter walking a known positive memory span). Preserve its
	// proved interval through a constant add when both endpoints wrap equally.
	// This is narrower than generic modular range analysis and adds no retained
	// expression facts.
	if !wide && op == opAdd {
		base, constant := left, right
		if base != nil && base.elemKind() == ekValue && base.st.kind == stConst {
			base, constant = right, left
		}
		if constant != nil && constant.elemKind() == ekValue && constant.st.kind == stConst {
			if minimum, maximum, ok := f.rootLoopAddressRange(base); ok {
				bias := uint64(uint32(constant.st.cval))
				low, high := minimum+bias, maximum+bias
				if low>>32 == high>>32 {
					return uint64(uint32(high))
				}
			}
		}
	}
	a := f.unsignedAddressMaximum(left, wide, budget)
	b := f.unsignedAddressMaximum(right, wide, budget)
	constant := uint64(0)
	known := right != nil && right.elemKind() == ekValue && right.st.kind == stConst && right.st.typ == typ
	if known {
		constant = uint64(right.st.cval)
	}
	return shared.UnsignedOperationMaximum(shared.IntOp(op), a, b, constant, known, wide)
}
