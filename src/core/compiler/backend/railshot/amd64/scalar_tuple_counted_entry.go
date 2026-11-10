// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import x64 "github.com/wago-org/wago/src/core/encoder/amd64"

// Initial counts of at least four can enter the proved group without peeling a
// scalar iteration. Reuse only the entire alignment region recorded when the
// private loop was opened; guessing a NOP boundary from a suffix is unsafe.
// Scalar zero/small counts and every remainder retain the original header.
func (f *fn) tryTupleCountedInitialGroup(counter Reg, wide bool, scalar, group int) bool {
	homes := f.loopLocalParams
	if homes.depth == 0 || int(homes.depth) > len(f.ctrl) {
		return false
	}
	fr := &f.ctrl[homes.depth-1]
	start := int(homes.entryPadStart)
	if fr.kind != cfLoop || fr.controlSite != scalar || fr.paramN < 1 || fr.paramN > 4 ||
		start >= scalar || scalar-start > 32 || scalar < 0 || group < scalar || group >= f.a.Len() {
		return false
	}
	var entry x64.Asm
	entry.AluRI(7, counter, 4, wide)
	branch := entry.JccPlaceholder(condAE)
	if scalar-start < entry.Len() {
		return false
	}
	for p := start; p < scalar; {
		n := scalarLoopNopLen(f.a.B[p:scalar])
		if n == 0 {
			return false
		}
		p += n
	}
	copy(f.a.B[start:scalar], entry.B)
	for p := start + entry.Len(); p < scalar; p++ {
		f.a.B[p] = 0x90
	}
	f.a.PatchRel32(start+branch, group)
	f.stats.peep("scalar-tuple-counted-initial-group")
	return true
}
