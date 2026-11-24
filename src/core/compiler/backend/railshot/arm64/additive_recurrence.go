// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var additiveRecurrenceEnabled = os.Getenv("WAGO_ARM64_NO_ADDITIVE_RECURRENCE") != "1"

type additiveRecurrence struct {
	cutoff, near, far uint32
}

// Match a pure recurrence whose base cases return the signed input and whose
// recursive case adds the two self-calls. Both steps must make progress without
// stepping below zero from a nonnegative recursive case.
func (f *fn) additiveRecurrence(c *wasm.Func) additiveRecurrence {
	if !additiveRecurrenceEnabled || !f.makesCalls || !f.opt(optStackFence) || f.skipFence ||
		f.compactNative() || f.a.ReserveBranch19 || f.moduleEH ||
		f.tracksGCFrameRoots() || len(f.customInstructions) != 0 || profileEnabled || regallocCheckEnabled ||
		f.stats != nil && f.stats.RecordSources || f.nLocals != 1 || f.nParams != 1 || len(f.ft.Results) != 1 ||
		f.localType[0] != mtI32 || mtOf(f.ft.Results[0]) != mtI64 || len(c.BodyBytes) > 64 {
		return additiveRecurrence{}
	}
	r := wasm.ReaderFrom(c.BodyBytes)
	take := func(bytes ...byte) bool {
		for _, want := range bytes {
			got, err := r.Byte()
			if err != nil || got != want {
				return false
			}
		}
		return true
	}
	if !take(0x20, 0, 0x41) { // local.get 0; i32.const
		return additiveRecurrence{}
	}
	cutoff, err := r.I32()
	if err != nil || cutoff < 2 || cutoff > 16 ||
		!take(0x48, 0x04, 0x7e, 0x20, 0, 0xac, 0x05) { // signed less-than; if i64; extend_s local 0; else
		return additiveRecurrence{}
	}
	step := func() uint32 {
		if !take(0x20, 0, 0x41) {
			return 0
		}
		n, err := r.I32()
		if err != nil || n < 1 || n > cutoff || n > 4 || !take(0x6b, 0x10) { // subtract; call
			return 0
		}
		callee, err := r.U32()
		if err != nil || callee != f.traceFuncIdx {
			return 0
		}
		return uint32(n)
	}
	a, b := step(), step()
	if a == 0 || b == 0 || !take(0x7c, 0x0b, 0x0b) || r.BytesLeft() != 0 { // i64.add; end if; end function
		return additiveRecurrence{}
	}
	return additiveRecurrence{uint32(cutoff), min(a, b), max(a, b)}
}

func (f *fn) emitAdditiveRecurrence(p additiveRecurrence, entry int) {
	const maxInput = 64
	start := f.a.Len()
	f.patchBranch26(entry, start)
	f.a.CmpImm32(X0, p.cutoff)
	small := f.a.Bcond(condB) // retain the original result, including signed negatives
	f.a.CmpImm32(X0, maxInput)
	large := f.a.Bcond(condA)
	// Require space even for the non-inlined original recursion before
	// replacing it. Preserve the original call-stack trap near the fence.
	f.ld64(X16, linMemReg, -72)
	f.a.MovImm64(X17, uint64(maxInput+1)*uint64(16+f.frameSize()))
	f.a.Adds64(X16, X16, X17)
	overflow := f.a.Bcond(condAE)
	f.a.CmpSP64(X16)
	low := f.a.Bcond(condB)
	for _, site := range [...]int{small, large, overflow, low} {
		f.recordPCRelative(site)
		f.patchBranch19(site, entry+4)
	}
	if f.interruptible {
		// The bounded recurrence skips recursive entries and their polls. Use
		// the normal entry to handle a pending interruption before running it.
		f.ld64(X16, linMemReg, -int32(offTrapCellPtr))
		f.ld32(X17, X16, 0)
		pending := f.zeroBranch(X17, false, false)
		f.recordPCRelative(pending)
		f.patchBranch19(pending, entry+4)
	}
	history := [...]Reg{X9, X10, X11, X12}
	for i := range p.far {
		f.a.MovImm64(history[i], uint64(p.cutoff-i-1))
	}
	f.a.SubImm32(X0, X0, p.cutoff-1)
	loop := f.a.Len()
	f.a.Add64(X13, history[p.near-1], history[p.far-1])
	for i := int(p.far) - 1; i > 0; i-- {
		f.a.MovReg64(history[i], history[i-1])
	}
	f.a.MovReg64(history[0], X13)
	f.a.SubsImm32(X0, X0, 1)
	back := f.a.Bcond(condNE)
	f.recordPCRelative(back)
	f.patchBranch19(back, loop)
	f.a.MovReg64(X0, X13)
	f.a.Ret()
	f.stats.peep("bounded-additive-recurrence")
}
