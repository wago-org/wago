// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "os"

var lonePredicateEnabled = os.Getenv("WAGO_AMD64_LONE_PREDICATE") != "0"
var borrowedPredicateEnabled = os.Getenv("WAGO_AMD64_BORROWED_PREDICATE") != "0"

// Older operands require full staging: canonical destinations may overwrite
// their existing spill homes. A lone owned root outside the local pin pool can
// remain tracked through convergence. A borrowed pinned local can also remain
// in its register when its current local state guarantees that value is live:
// convergence may store it, but only reloads a pin whose current state is mem.
// Distinct pinned locals have distinct registers and cannot overwrite it.
// Borrowed values are retained on loop backedges, where the saved transfers
// recur; forward branches retain the existing code layout and staging policy.
func (f *fn) flushBranchPredicate(predicate *elem, loopBackedge bool) {
	if lonePredicateEnabled && f.depth() == 1 && predicate.isValue() && predicate.st.typ == mtI32 && !predicate.st.hasGCRoot() && f.lonePredicateRegisterSafe(predicate, loopBackedge) {
		f.flushBelow(predicate)
		f.stats.peep("lone-predicate")
		if predicate.st.kind == stLocalReg {
			f.stats.peep("lone-predicate-borrowed")
		}
		return
	}
	f.flush()
}

func (f *fn) lonePredicateRegisterSafe(predicate *elem, loopBackedge bool) bool {
	s := predicate.st
	if s.kind == stReg {
		return !f.pinnedLocalMask.has(s.reg)
	}
	if !borrowedPredicateEnabled || !loopBackedge || s.kind != stLocalReg || int(s.idx) >= len(f.locals) || f.pinRelinquished {
		return false
	}
	local := f.locals[s.idx]
	return local.reg == s.reg && !local.isFloat && (local.state == lsReg || local.state == lsStackReg)
}
