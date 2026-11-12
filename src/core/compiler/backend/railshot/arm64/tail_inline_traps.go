// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

// Parameter assignments are private and cannot be observed by a trap. With one
// trapping argument expression, destination-directed evaluation cannot reorder
// observable failures. Prefix traps, multiple effects and guard-backed loads
// retain the ordinary eager materialization path.
func (f *fn) canDeferInlineTailTrap(index uint32) bool {
	if f.inlineTailLoopFrame == 0 || index != f.traceFuncIdx || f.guardMode || !f.opt(optBorrowedDivRem) {
		return false
	}
	target := f.inlineTargets.target(int(index))
	if target == nil || !target.tailLoop() {
		return false
	}
	roots := f.rootsBottomToTop()
	params := target.paramCount()
	if params == 0 || len(roots) < params {
		return false
	}
	start := f.s.baseOfValentBlock(roots[len(roots)-params])
	inside, count, argTrap := false, 0, false
	for e := f.s.next(f.s.head); e != f.s.head; e = f.s.next(e) {
		if e == start {
			inside = true
		}
		if e.isDeferred() && isDivRem(e.deferredOp()) {
			count++
			argTrap = inside
			if count > 1 {
				return false
			}
		}
	}
	return count == 1 && argTrap
}
