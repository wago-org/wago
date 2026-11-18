// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var modularLoadGroupEnabled = os.Getenv("WAGO_AMD64_MODULAR_LOAD_GROUP") != "0"

// modularLoadAddress recognizes wrapping i32 source arithmetic, not the
// widened memory immediate. The bounded group currently covers word loads.
func modularLoadAddress(e *elem) (local uint32, displacement uint8, ok bool) {
	if e != nil && e.isValue() && (e.st.kind == stLocalReg || e.st.kind == stLocalRef) && e.st.typ == mtI32 {
		return e.st.idx, 0, true
	}
	if e == nil || !e.isDeferred() || e.deferredOp() != opAdd || e.valueType() != mtI32 {
		return
	}
	a, b := e.arg0, e.arg1
	if a.isValue() && a.st.kind == stConst {
		a, b = b, a
	}
	if !a.isValue() || (a.st.kind != stLocalReg && a.st.kind != stLocalRef) ||
		!b.isValue() || b.st.kind != stConst || b.st.typ != mtI32 {
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

func (f *fn) modularLoadGroupCovers(local uint32, displacement uint8) bool {
	if f.boundsCertSources&boundsCertSourceBit(3, local) == 0 {
		return false
	}
	for i := range f.boundsCerts {
		c := &f.boundsCerts[i]
		if c.kind == 3 && c.idx == local {
			lo, hi := uint8(c.extent), uint8(uint32(c.extent)>>8)
			return displacement >= lo && displacement <= hi && (displacement-lo)&3 == 0
		}
	}
	return false
}

func (f *fn) addModularLoadGroup(g modularLoadGroup) {
	f.boundsCertSources |= boundsCertSourceBit(3, g.local)
	slot := -1
	for i := range f.boundsCerts {
		c := &f.boundsCerts[i]
		if c.kind == 3 && c.idx == g.local {
			slot = i
			break
		}
		if slot < 0 && c.kind == 0 {
			slot = i
		}
	}
	if slot < 0 {
		slot = int(f.nextBoundsCert) % len(f.boundsCerts)
		f.nextBoundsCert++
	}
	f.boundsCerts[slot] = boundsCert{kind: 3, idx: g.local, extent: int32(g.low) | int32(g.high)<<8}
}

// checkModularLoadGroup tests the exact set of adjacent word accesses. With no
// dynamic wrap, the highest word suffices. If the sequence wraps, a valid set
// requires full 4-GiB memory and word-aligned addresses; otherwise a member of
// the dense set traps. No source state or memory is changed by this check.
func (f *fn) checkModularLoadGroup(ea Reg, g modularLoadGroup) {
	f.pinned = f.pinned.add(ea)
	low := f.allocReg(0)
	f.a.MovRegReg32(low, ea)
	if delta := int32(g.low) - int32(g.first); delta != 0 {
		f.a.AluRI(0, low, delta, false)
	}
	high := f.allocReg(maskOf(low))
	limit := f.allocReg(maskOf(low, high))
	f.a.LeaDisp(high, low, int32(g.high-g.low)+4)
	// The common case needs no explicit 4-GiB test: if the widened end is
	// within memory, none of the grouped dynamic addresses wrapped.
	f.a.Cmp64(high, f.memSizeReg)
	done := f.a.JccPlaceholder(condBE)
	f.a.MovImm64(limit, 1<<32)
	f.a.Cmp64(f.memSizeReg, limit)
	f.trapIf(condNE, trapMemOOB)
	f.a.AluRI(5, high, 4, true)
	f.a.Cmp64(high, limit)
	f.trapIf(condB, trapMemOOB)
	f.a.TestImm(low, 3, false)
	f.trapIf(condNE, trapMemOOB)
	f.a.PatchRel32(done, f.a.Len())
	f.release(limit)
	f.release(high)
	f.release(low)
	f.pinned = f.pinned.remove(ea)
	f.stats.peep("modular-load-group")
}
