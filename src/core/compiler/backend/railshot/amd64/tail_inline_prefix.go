// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

// Constants and the caller's dedicated integer local homes survive a call-free
// tail-loop splice unchanged. Hide such a bounded prefix from the callee's
// private operand stack instead of spilling it at each synthetic control merge.
// Owned registers, slots, references and callee-local aliases keep the normal
// path, where arbitrary control and register pressure have canonical homes.
func (f *fn) detachInlineTailPrefix(t *inlineTarget, base int, saved *[8]storage) int {
	if !t.tailLoop() || f.makesCalls {
		return 0
	}
	roots := f.rootsBottomToTop()
	if len(roots) == 0 || len(roots) > len(saved) {
		return 0
	}
	for i, root := range roots {
		st := root.st
		if !root.isValue() || st.typ != mtI32 && st.typ != mtI64 {
			return 0
		}
		switch st.kind {
		case stConst:
		case stLocalReg:
			if st.index() >= base {
				return 0
			}
			reg, fp, ok := f.pinReg(st.index())
			if !ok || fp || reg != st.reg {
				return 0
			}
		default:
			return 0
		}
		saved[i] = st
	}
	for _, root := range roots {
		f.erase(root)
	}
	f.stats.peep("inline-prefix-retained")
	return len(roots)
}

func (f *fn) restoreInlineTailPrefix(t *inlineTarget, prefix []storage) {
	// A declared result does not imply a reachable return. Keep the inline
	// body's unreachable state and discard its caller prefix on this dead edge.
	if f.unreachable {
		return
	}

	var result storage
	hasResult := t.resultTypeEnd != t.localTypeEnd
	if hasResult {
		root := f.s.back()
		// A slot belongs to the callee's logical stack. Capture it before
		// restoring the caller's different canonical slot positions.
		if root.st.kind == stSlot {
			f.materialize(root)
		}
		result = root.st
		f.erase(root)
	}
	for _, st := range prefix {
		f.pushValue(st)
	}
	if hasResult {
		root := f.pushValue(result)
		if result.kind == stReg {
			f.regUser[result.reg] = root
		}
	}
}
