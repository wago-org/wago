// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

// A simple nontrapping immediate argument can be emitted after parallel moves
// when its permanent source pin is outside the argument/swap scratch banks.
// Regional pins and nested producers retain ordinary eager materialization.
func (f *fn) pinnedImmediateCallArgument(root *elem) (source Reg, immediate int64, wide bool, ok bool) {
	if f.intervalActive != 0 || root == nil || !root.isDeferred() || (root.deferredOp() != opAdd && root.deferredOp() != opSub) ||
		(root.st.typ != mtI32 && root.st.typ != mtI64) {
		return regNone, 0, false, false
	}
	left, right := f.s.arg0(root), f.s.arg1(root)
	if left == nil || right == nil || left.elemKind() != ekValue || left.st.kind != stLocalReg ||
		right.elemKind() != ekValue || right.st.kind != stConst {
		return regNone, 0, false, false
	}
	source = left.st.reg
	if source < X19 || source > X27 || source == linMemReg || !f.pinnedLocalMask.has(source) {
		return regNone, 0, false, false
	}
	immediate = right.st.cval
	if root.deferredOp() == opSub {
		immediate = -immediate
	}
	return source, immediate, root.st.typ.is64(), f.fitsAddSubImmediate(immediate)
}
