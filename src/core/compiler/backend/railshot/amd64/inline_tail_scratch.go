// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

// R8 is normally kept for bulk/table/GC helpers and incoming arguments. A
// call-free, non-polling pure numeric caller with at most three parameters has
// none of those fixed roles. Its single private tail callee can use R8 before
// consuming RBP, preserving the ordinary single-result merge register.
func (f *fn) inlineTailSpareR8() bool {
	return f.regMerge && f.opt(optRegMerge) && !profileEnabled && f.nParams <= 3 &&
		!f.makesCalls && !f.interruptible && !f.moduleEH && !f.moduleHasSIMD &&
		f.gcFrameRoots == nil && len(f.gcTypeLayouts) == 0 && len(f.customInstructions) == 0 &&
		len(f.intervalReg) == 0 && !f.vectorRegion.enabled &&
		!f.reserved.has(R8) && !f.pinnedLocalMask.has(R8) && !f.pinnedLocalMask.has(mergeReg) && f.regUser[R8] == nil &&
		f.m != nil && len(f.m.Memories) == 0 && len(f.m.Tables) == 0 && len(f.m.Globals) == 0
}
