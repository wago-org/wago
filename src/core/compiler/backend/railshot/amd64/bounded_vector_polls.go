// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

// emitBoundedInterruptPoll checks after a bounded batch of independent-lane groups,
// including the final group. All guest stores are committed before this point.
// The following explicit TEST recreates flags clobbered by the check.
func (e *regionLoopEmitter) emitBoundedInterruptPoll() {
	if !boundedVectorPolls || !e.f.interruptible {
		return
	}
	f := e.f
	scratch := e.gp[0]
	// Stream caching may own every GP register. Preserve this exact register in
	// a dedicated compiler spill, rather than guessing that RSI remains free.
	f.a.Store64(RSP, f.spillOff(e.pollSlot), scratch)
	oldPC := f.wasmPC
	f.wasmPC = e.pollPC
	f.emitInterruptCheck(scratch)
	f.wasmPC = oldPC
	f.a.Load64(scratch, RSP, f.spillOff(e.pollSlot))
	f.stats.peep("bounded-vector-poll")
}
