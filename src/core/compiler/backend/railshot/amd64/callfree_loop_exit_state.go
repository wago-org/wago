// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

// The first edge out of a proved call-free loop may keep integer pins in their
// registers. The existing merge machinery restores that same contract on every
// later edge and spills dirty pins before a subsequent call. This avoids a frame
// store solely to cross the immediately enclosing void block.
func (f *fn) retainCallFreeLoopExitRegisters(fi int) bool {
	if !f.opt(optLoopRegState) || fi != len(f.ctrl)-2 || fi < 0 || f.moduleEH || f.gcFrameRoots != nil ||
		len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.pinRelinquished ||
		profileEnabled && f.stats != nil && f.stats.RecordSources {
		return false
	}
	loop, outer := &f.ctrl[len(f.ctrl)-1], &f.ctrl[fi]
	if loop.kind != cfLoop || loop.paramN != 0 || loop.resultN != 0 || outer.kind != cfBlock || outer.branchArity() != 0 ||
		f.frameBranchState(outer) != nil || len(f.pinnedLocals) == 0 {
		return false
	}
	for _, local := range f.locals {
		if local.state == lsConstZero {
			return false
		}
	}
	for _, x := range f.pinnedLocals {
		if (f.localType[x] != mtI32 && f.localType[x] != mtI64) || f.locals[x].state == lsMem || f.locals[x].state == lsConstZero || f.locals[x].reg == regNone {
			return false
		}
	}
	state := f.newLocStateBuf()
	for i := range state {
		state[i] = lsReg
	}
	f.setFrameBranchState(outer, state)
	f.stats.peep("callfree-loop-exit-register-state")
	return true
}
