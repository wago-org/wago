// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "math/bits"

// x*(2^k+1)+c needs no multiply or materialized constants when c is an
// add/sub immediate. Two dependent ADDs have a shorter latency chain than MADD
// and avoid the literal-register setup. All operations retain lane wraparound.
func (f *fn) tryMulAddAffineImmediate(node, mul, addend *elem, dest Reg, w bool) Reg {
	if (node.deferredOp() != opAdd && (node.deferredOp() != opSub || f.s.arg0(node) != mul)) || addend.elemKind() != ekValue || addend.st.kind != stConst {
		return regNone
	}
	add := addend.st.cval
	if !w {
		add = int64(int32(add))
	}
	if node.deferredOp() == opSub {
		add = -add
	}
	if !f.fitsAddSubImmediate(add) {
		return regNone
	}
	source, constant := f.s.arg0(mul), f.s.arg1(mul)
	if constant.elemKind() != ekValue || constant.st.kind != stConst {
		source, constant = constant, source
	}
	if constant.elemKind() != ekValue || constant.st.kind != stConst || source.elemKind() != ekValue {
		return regNone
	}
	factor := uint64(constant.st.cval)
	if !w {
		factor = uint64(uint32(factor))
	}
	if factor <= 2 || (factor-1)&(factor-2) != 0 {
		return regNone
	}
	shift := uint8(bits.TrailingZeros64(factor - 1))
	previousPins := f.pinned
	reg, owned := f.materializeRead(source)
	f.pinned = f.pinned.add(reg)
	result := dest
	if result == regNone {
		if owned {
			result = reg
		} else {
			result = f.allocReg(maskOf(reg))
		}
	}
	f.a.AddShifted(result, reg, reg, shift, !w)
	if add != 0 && !f.aluImm(opAdd, result, add, w) {
		panic("proved affine immediate was not encodable")
	}
	f.pinned = previousPins
	if owned && result != reg {
		f.release(reg)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	f.stats.peep("mul-add-affine-immediate")
	return result
}
