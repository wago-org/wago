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

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var modularLoadGroupEnabled = os.Getenv("WAGO_ARM64_MODULAR_LOAD_GROUP") != "0"

// modularLoadAddress recognizes wrapping i32 source arithmetic, not the
// widened memory immediate. The bounded group currently covers word loads.
func (f *fn) modularLoadAddress(e *elem) (local uint32, displacement uint8, ok bool) {
	if f.modularBoundsLive && e != nil && e.elemKind() == ekValue && (e.st.kind == stLocalReg || e.st.kind == stLocalRef) && e.st.typ == mtI32 {
		return e.st.idx, 0, true
	}
	if e == nil || !e.isDeferred() || e.deferredOp() != opAdd || e.st.typ != mtI32 {
		return
	}
	a, b := f.s.arg0(e), f.s.arg1(e)
	if a.elemKind() == ekValue && a.st.kind == stConst {
		a, b = b, a
	}
	if a.elemKind() != ekValue || (a.st.kind != stLocalReg && a.st.kind != stLocalRef) ||
		a.st.typ != mtI32 || b.elemKind() != ekValue || b.st.kind != stConst || b.st.typ != mtI32 {
		return
	}
	v := uint32(b.st.cval)
	if v >= 256 || v&3 != 0 {
		return
	}
	return a.st.idx, uint8(v), true
}

type modularLoadGroup struct {
	local            uint32
	first, low, high uint8
	valid, covered   bool
}

func (f *fn) scanModularLoadGroup(r *wasm.Reader, local uint32, first uint8) modularLoadGroup {
	mask := shared.ScanModularWordLoads(r, local, f.localBase, first, &f.boundsScanWork, maxFunctionRangeOps)
	return finishModularLoadGroup(local, first, mask)
}

func finishModularLoadGroup(local uint32, first uint8, mask uint64) modularLoadGroup {
	low, high, ok := shared.DenseModularWordSpan(mask)
	if !ok {
		return modularLoadGroup{}
	}
	return modularLoadGroup{local: local, first: first, low: low, high: high, valid: true}
}

const modularBoundsKind = 3

func (f *fn) modularLoadGroupCovers(local uint32, displacement uint8) bool {
	if !f.modularBoundsLive {
		return false
	}
	extent := f.boundsCertExtent(modularBoundsKind, local)
	lo, hi := uint8(extent), uint8(uint32(extent)>>8)
	return extent != 0 && displacement >= lo && displacement <= hi && (displacement-lo)&3 == 0
}

// checkModularLoadGroup tests the exact set of adjacent word accesses. With no
// dynamic wrap, the highest word suffices. If the sequence wraps, a valid set
// requires full 4-GiB memory and word-aligned addresses; otherwise a member of
// the dense set traps. No source state or memory is changed by this check.
func (f *fn) checkModularLoadGroup(ea Reg, g modularLoadGroup) {
	pins := f.pinned
	f.pinned = f.pinned.add(ea)
	low := f.allocReg(0)
	switch delta := int32(g.low) - int32(g.first); {
	case delta > 0:
		f.a.AddImm32(low, ea, uint32(delta))
	case delta < 0:
		f.a.SubImm32(low, ea, uint32(-delta))
	default:
		f.a.MovReg32(low, ea)
	}
	high := f.allocReg(maskOf(low))
	// Allocation may spill a live value. Keep that spill before the split,
	// where both paths execute it and agree with the allocator at the join.
	limit := f.allocReg(maskOf(low, high))
	f.a.AddImm64(high, low, uint32(g.high-g.low)+4)
	f.cmpRR(high, f.memSizeReg, true)
	done := f.a.Bcond(condBE)
	f.a.MovImm64(limit, 1<<32)
	f.cmpRR(f.memSizeReg, limit, true)
	f.trapIf(condNE, trapMemOOB)
	f.a.SubImm64(high, high, 4)
	f.cmpRR(high, limit, true)
	f.trapIf(condB, trapMemOOB)
	f.a.TstImm32(low, 3)
	f.trapIf(condNE, trapMemOOB)
	f.patchBranch19(done, f.a.Len())
	f.release(limit)
	f.release(high)
	f.release(low)
	f.pinned = pins
	f.stats.peep("modular-load-group")
}
