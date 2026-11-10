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

// Older operands require full staging: canonical destinations may overwrite
// their existing spill homes. With one owned numeric root outside the local pin
// pool, no prefix exists and local convergence cannot restore its register.
// It remains tracked and spillable until popBranchCondition after convergence.
func (f *fn) flushBranchPredicate(predicate *elem) {
	if lonePredicateEnabled && f.depth() == 1 && predicate.isValue() && predicate.st.kind == stReg && predicate.st.typ == mtI32 && !f.pinnedLocalMask.has(predicate.st.reg) && !predicate.st.hasGCRoot() {
		f.flushBelow(predicate)
		f.stats.peep("lone-predicate")
		return
	}
	f.flush()
}
