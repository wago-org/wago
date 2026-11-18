// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/encoder/amd64"
	"math/bits"
)

// integerMapGuardEmitter emits only nontrapping checks. The caller supplies
// four distinct leased GP registers, a memory-size register, and frame slots
// containing zero-extended entry locals. It owns no allocator state.
type integerMapGuardEmitter struct {
	a          *amd64.Asm
	plan       integerMapGuards
	gp         [4]Reg
	memorySize Reg
	slotBase   int32
	failures   [96]int
	failureN   int
}

func (e *integerMapGuardEmitter) slot(i int) int32    { return e.slotBase + int32(i*8) }
func (e *integerMapGuardEmitter) countSlot() int      { return int(e.plan.localN) }
func (e *integerMapGuardEmitter) startSlot(i int) int { return e.countSlot() + 1 + 2*i }
func (e *integerMapGuardEmitter) endSlot(i int) int   { return e.startSlot(i) + 1 }
func (e *integerMapGuardEmitter) fail(cc Cond) {
	e.failures[e.failureN] = e.a.JccPlaceholder(cc)
	e.failureN++
}
func (e *integerMapGuardEmitter) patchFallback(target int) {
	for _, site := range e.failures[:e.failureN] {
		e.a.PatchRel32(site, target)
	}
}

// shareStream selects an earlier same-stride stream with a small positive
// constant displacement. The guard checks that modular entry addresses did not
// wrap before using the common widened pointer in a vector packet.
func shareStream(m *integerMapMemoryPlan, i int) (int, int32) {
	s := m.streams[i]
	if s.stride == 0 {
		return i, 0
	}
	for j := 0; j < i; j++ {
		b := m.streams[j]
		if b.stride != s.stride || b.offset != s.offset || b.address.coeff != s.address.coeff {
			continue
		}
		delta := s.address.constant - b.address.constant
		if delta > 0 && delta <= 127 {
			base, parentDelta := shareStream(m, j)
			return base, parentDelta + int32(delta)
		}
	}
	return i, 0
}

// affine emits the exact modular i32 entry expression, zero-extending its
// result through 32-bit destinations. tmp is distinct from dst.
func (e *integerMapGuardEmitter) affine(x integerMapAffine, dst, tmp Reg) {
	e.a.MovImm32(dst, int32(x.constant))
	for i, c := range x.coeff {
		if c == 0 {
			continue
		}
		e.a.Load32(tmp, RSP, e.slot(i))
		if c != 1 {
			e.a.ImulRI(tmp, int32(c), false)
		}
		e.a.Add32(dst, tmp)
	}
}

func (e *integerMapGuardEmitter) emit() {
	a, b, c, d := e.gp[0], e.gp[1], e.gp[2], e.gp[3]
	g := &e.plan
	e.affine(g.limit, a, b)
	e.a.Load32(b, RSP, e.slot(int(g.counter)))
	e.a.Cmp32(a, b)
	e.fail(condBE) // zero, negative, and wrapping trips keep the scalar loop
	e.a.Sub32(a, b)
	if g.step != 1 {
		e.a.TestImm(a, g.step-1, false)
		e.fail(condNE)
		e.a.ShiftImm(5, a, uint8(bits.TrailingZeros32(g.step)), false)
	}
	if g.minTrip != 0 {
		e.a.AluRI(7, a, int32(g.minTrip), false)
		e.fail(condB)
	}
	e.a.Store32(RSP, e.slot(e.countSlot()), a)
	for i, s := range g.memory.streams[:g.memory.streamN] {
		e.affine(s.address, a, d)
		e.a.Load32(b, RSP, e.slot(e.countSlot()))
		e.a.AluRI(5, b, 1, false)
		if s.stride == 0 {
			e.a.Xor32(b, b)
		} else if s.stride != 1 {
			e.a.ImulRI(b, int32(s.stride), true)
		}
		e.a.AluRI(0, b, 4, true)
		e.a.MovReg64(c, a)
		e.a.Add64(c, b)
		e.a.MovImm64(d, 1<<32)
		e.a.Cmp64(c, d)
		e.fail(condA) // source dynamic addresses must not wrap across the packet
		if s.offset != 0 {
			e.a.MovImm64(d, s.offset)
			e.a.Add64(a, d)
			e.a.Add64(c, d)
		}
		e.a.Cmp64(c, e.memorySize)
		e.fail(condA)
		e.a.Store64(RSP, e.slot(e.startSlot(i)), a)
		e.a.Store64(RSP, e.slot(e.endSlot(i)), c)
	}
	for i := range g.memory.streams[:g.memory.streamN] {
		base, delta := shareStream(&g.memory, i)
		if base == i {
			continue
		}
		e.a.Load64(a, RSP, e.slot(e.startSlot(base)))
		e.a.AluRI(0, a, delta, true)
		e.a.Load64(b, RSP, e.slot(e.startSlot(i)))
		e.a.Cmp64(a, b)
		e.fail(condNE)
	}
	for _, pair := range g.memory.pairs[:g.memory.pairN] {
		equal := -1
		if !g.packStores && g.memory.streams[pair.a].stride != 0 && g.memory.streams[pair.a].stride == g.memory.streams[pair.b].stride {
			e.a.Load64(a, RSP, e.slot(e.startSlot(int(pair.a))))
			e.a.Load64(b, RSP, e.slot(e.startSlot(int(pair.b))))
			e.a.Cmp64(a, b)
			equal = e.a.JccPlaceholder(condE)
		}
		e.a.Load64(a, RSP, e.slot(e.endSlot(int(pair.a))))
		e.a.Load64(b, RSP, e.slot(e.startSlot(int(pair.b))))
		e.a.Cmp64(a, b)
		disjoint := e.a.JccPlaceholder(condBE)
		e.a.Load64(a, RSP, e.slot(e.endSlot(int(pair.b))))
		e.a.Load64(b, RSP, e.slot(e.startSlot(int(pair.a))))
		e.a.Cmp64(a, b)
		e.fail(condA)
		e.a.PatchRel32(disjoint, e.a.Len())
		if equal >= 0 {
			e.a.PatchRel32(equal, e.a.Len())
		}
	}
}
