// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var boundedBranchIndexEnabled = os.Getenv("WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX") != "1"

// Capture only a direct Wasm i32 constant or AND mask before normal index
// materialization. Every operand still executes, including pending traps.
// No range fact survives a local assignment or control join.
func (f *fn) branchIndexUpperBound(e *elem) (uint32, bool) {
	if e.st.typ != mtI32 {
		return 0, false
	}
	if e.elemKind() == ekValue && e.st.kind == stConst {
		return uint32(e.st.cval), true
	}
	if !e.isDeferred() || e.deferredOp() != opAnd {
		return 0, false
	}
	for _, id := range [...]nodeID{e.child0ID(), e.child1ID()} {
		child := f.s.node(id)
		if child.elemKind() == ekValue && child.st.kind == stConst && child.st.typ == mtI32 {
			return uint32(child.st.cval), true
		}
	}
	return 0, false
}
