// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var maskedZeroCompareEnabled = os.Getenv("WAGO_ARM64_NO_MASKED_ZERO_COMPARE") != "1"

// Explicit equality/inequality with zero has the same Z-flag predicate as eqz
// or a direct bitmask condition. Admit only a literal typed zero; the existing
// mask lowering retains RHS-first realization and all memory/trap checks.
func (f *fn) maskedZeroCompare(node *elem) (*elem, Cond, bool) {
	if !maskedZeroCompareEnabled || node == nil || !node.isDeferred() || node.st.typ != mtI32 && node.st.typ != mtI64 || node.deferredOp() != opEq && node.deferredOp() != opNe {
		return nil, 0, false
	}
	a, b := f.s.arg0(node), f.s.arg1(node)
	zero := func(e *elem) bool {
		if e == nil || e.elemKind() != ekValue || e.st.kind != stConst || e.st.typ != node.st.typ {
			return false
		}
		if node.st.typ == mtI32 {
			return uint32(e.st.cval) == 0
		}
		return e.st.cval == 0
	}
	inner := a
	if !zero(b) {
		if !zero(a) {
			return nil, 0, false
		}
		inner = b
	}
	if inner == nil || !inner.isDeferred() || inner.deferredOp() != opAnd || inner.st.typ != node.st.typ {
		return nil, 0, false
	}
	cc := condE
	if node.deferredOp() == opNe {
		cc = condNE
	}
	return inner, cc, true
}
