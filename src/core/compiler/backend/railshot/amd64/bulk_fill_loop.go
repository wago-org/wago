// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

var bulkMemoryLoopsEnabled = true
var bulkCopyVectorEnabled = true

// RDI/RCX are the checked destination/count; RAX holds the replicated low byte.
// CPUs without AVX2 retain the byte string-store path.
func (f *fn) emitBulkFill(vec Reg) {
	if f.opt(optBulkMemoryLoops) && f.cpuHas(shared.AMD64AVX2) {
		f.emitVectorBulkFill(vec)
		return
	}
	f.a.RepStosb()
}

// The checked original range is at least 256 bytes. An aligned middle and a
// fixed-size store group ending exactly at the checked end cover every byte,
// including a non-vector-sized remainder, without starting a short REP tail.
func (f *fn) emitVectorBulkFill(vec Reg) {
	wide := f.cpuHas(shared.AMD64AVX512)
	minimum := int32(256)
	if wide {
		minimum = 512
	}
	f.a.AluRI(cmpDigit, RCX, minimum, true)
	small := f.a.JccPlaceholder(condB)
	width, group := int32(32), int32(128)
	f.a.MovGprToXmm(vec, RAX, true)
	if wide {
		f.a.ZSIMDRR(2, 1, 0x59, true, vec, vec)
		width, group = 64, 512
	} else {
		f.a.Punpcklqdq(vec, vec)
		f.a.YInsertI128(vec, vec, vec, 1)
	}
	store := func(base Reg, off int32) {
		if wide {
			f.a.ZMovdqu64StoreIdx(base, RSP, vec, off)
		} else {
			f.a.YMovdquStoreDisp(base, off, vec)
		}
	}
	f.a.LeaScaled(R8, RDI, RCX, 0, -group)
	f.a.MovReg64(RDX, RDI)
	f.a.AluRI(4, RDX, width-1, true)
	aligned := f.a.JccPlaceholder(condE)
	store(RDI, 0)
	f.a.Neg(RDX, true)
	f.a.AluRI(0, RDX, width, true)
	f.a.Add64(RDI, RDX)
	f.a.PatchRel32(aligned, f.a.Len())
	f.a.Cmp64(RDI, R8)
	tail := f.a.JccPlaceholder(condAE)
	f.a.AlignLoop32()
	loop := f.a.Len()
	for off := int32(0); off < group; off += width {
		if wide {
			f.a.ZMovdqa64StoreIdx(RDI, RSP, vec, off)
		} else {
			store(RDI, off)
		}
	}
	f.a.AluRI(0, RDI, group, true)
	f.a.Cmp64(RDI, R8)
	f.a.PatchRel32(f.a.JccPlaceholder(condB), loop)
	f.a.PatchRel32(tail, f.a.Len())
	for off := int32(0); off < group; off += width {
		store(R8, off)
	}
	f.a.VZeroUpper()
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(small, f.a.Len())
	f.a.TestSelf(RCX, true)
	empty := f.a.JccPlaceholder(condE)
	f.a.RepStosb()
	f.a.PatchRel32(empty, f.a.Len())
	f.a.PatchRel32(done, f.a.Len())
	f.stats.peep("bulk-memory-vector-end-fill")
}
