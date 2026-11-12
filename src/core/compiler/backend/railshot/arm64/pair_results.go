// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (f *fn) directIntegerPairCandidate(c *wasm.Func) bool {
	if profileEnabled || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || f.makesCalls || len(f.ft.Results) != 2 || !sigIsIntOnly(f.ft) || f.nLocals > 8 || len(c.BodyBytes) > 64 {
		return false
	}
	for _, run := range c.Locals.Runs {
		if run.Type != wasm.I32 && run.Type != wasm.I64 {
			return false
		}
	}
	// Every normal exit is the final function end; explicit returns, branches,
	// calls, loops and conditional control retain canonical result slots.
	return shared.IsStraightLineBlock(wasm.NewReader(c.BodyBytes), f.classifier)
}

func (f *fn) placeDirectIntegerPair() {
	roots := f.rootsBottomToTop()
	previousPins := f.pinned
	f.pinned = f.pinned.add(X0).add(X1)
	var regs [2]Reg
	// Realize both results before overwriting either incoming argument register.
	for i := range regs {
		reg := f.materialize(roots[i])
		if reg == X0 || reg == X1 {
			safe := f.allocReg(maskOf(X0, X1))
			f.a.MovReg64(safe, reg)
			f.release(reg)
			f.occupy(roots[i], safe)
			reg = safe
		}
		regs[i] = reg
		f.pinned = f.pinned.add(reg)
	}
	var writes regallocWriteMask
	if regallocCheckEnabled {
		writes = f.checkTerminalWrites()
	}
	for i, reg := range regs {
		if f.ft.Results[i] == wasm.I32 {
			f.a.MovReg32(Reg(i), reg)
		} else {
			f.a.MovReg64(Reg(i), reg)
		}
	}
	for i, reg := range regs {
		f.release(reg)
		f.erase(roots[i])
	}
	if regallocCheckEnabled {
		f.checkRestoreWrites(writes)
	}
	f.pinned = previousPins
	f.stats.peep("direct-integer-pair-results")
}
