// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/encoder/amd64"

// integerMapBodyEmitter consumes only a guard-qualified candidate. Scratch
// registers and frame slots are leased by the caller; it does not allocate or
// change compiler ownership. Initial recipes use two or four i32 lanes and SSE4.1.
type integerMapBodyEmitter struct {
	a            *amd64.Asm
	plan         *integerMapPlan
	guard        *integerMapGuardEmitter
	recipe       *integerMapRecipe
	slp          *integerMapSLP
	slpWide      bool
	facts        *[integerMapMaxNodes + 1]integerMapAffine
	induction    integerMapInduction
	gp           [4]Reg
	fp           [16]Reg
	increment    Reg
	hasIncrement bool
	addressBase  [2]int
	addressReg   [2]Reg
	addressN     uint8
	memoryBase   Reg
	outputBase   int32
	cacheBase    [2]int
	cacheUsed    [2]bool
}

// integerMapBodySupported is checked before emitting any native state. The
// first lowering handles contiguous words, invariant loads, and stride-two
// word streams; all other legal candidates retain their scalar path.
func integerMapBodySupported(p *integerMapPlan, g *integerMapGuards, r *integerMapRecipe) bool {
	if r.needsAVX2 || r.registers < 2 || r.registers > 16 || (g.lanes != 2 && g.lanes != 4) {
		return false
	}
	for _, s := range g.memory.streams[:g.memory.streamN] {
		if s.stride != 0 && s.stride != 4 && s.stride != 8 {
			return false
		}
	}
	for i := 1; i <= int(p.nodeN); i++ {
		if !r.used[i] {
			continue
		}
		n := p.nodes[i]
		switch {
		case n.op == 0x20 || n.op == 0x41 || n.op == 0x28 || n.op == 0x1b || n.op == 0x45:
		case n.op >= 0x46 && n.op <= 0x4f || n.op >= 0x6a && n.op <= 0x6c || n.op >= 0x71 && n.op <= 0x73:
		case n.op >= 0x74 && n.op <= 0x76:
			if p.nodes[n.b].op != 0x41 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (e *integerMapBodyEmitter) vector(id uint8) Reg { return e.fp[e.recipe.reg[id]] }
func (e *integerMapBodyEmitter) copy(dst, src Reg) {
	if dst != src {
		e.a.SseRR(0x66, 0x6f, dst, src, false)
	}
}
func (e *integerMapBodyEmitter) scratch() (Reg, Reg) {
	return e.fp[e.recipe.registers-2], e.fp[e.recipe.registers-1]
}
func (e *integerMapBodyEmitter) broadcast(dst Reg, value uint32) {
	e.a.MovImm32(e.gp[2], int32(value))
	e.a.MovGprToXmm(dst, e.gp[2], false)
	e.a.Pshufd(dst, dst, 0)
}
func (e *integerMapBodyEmitter) shift(dst Reg, digit byte, count byte) {
	e.a.SseMapRRI(0x66, 0, 0x72, Reg(digit), dst, count)
}

// compare emits exact Wasm 0/1 lanes. Temporary masks may be all-ones, but no
// mask representation leaks to a source local, store, or other arithmetic.
func (e *integerMapBodyEmitter) compare(out, left, right Reg, op byte) {
	t, u := e.scratch()
	invert := false
	switch op {
	case 0x45:
		e.a.SseRR(0x66, 0xef, t, t, false)
		e.a.SseRR(0x66, 0x76, t, left, false)
	case 0x46, 0x47:
		e.copy(t, left)
		e.a.SseRR(0x66, 0x76, t, right, false)
		invert = op == 0x47
	case 0x48, 0x4e:
		e.copy(t, right)
		e.a.SseRR(0x66, 0x66, t, left, false)
		invert = op == 0x4e
	case 0x4a, 0x4c:
		e.copy(t, left)
		e.a.SseRR(0x66, 0x66, t, right, false)
		invert = op == 0x4c
	case 0x49, 0x4b, 0x4d, 0x4f:
		// Bias unsigned lanes into signed order. Both operands are captured before
		// out is overwritten, because the local schedule may reuse an input.
		e.copy(t, left)
		e.copy(u, right)
		e.broadcast(out, 0x80000000)
		e.a.SseRR(0x66, 0xef, t, out, false)
		e.a.SseRR(0x66, 0xef, u, out, false)
		if op == 0x49 || op == 0x4f {
			e.a.SseRR(0x66, 0x66, u, t, false)
			t, u = u, t
			invert = op == 0x4f
		} else {
			e.a.SseRR(0x66, 0x66, t, u, false)
			invert = op == 0x4d
		}
	default:
		panic("unsupported integer map comparison")
	}
	if invert {
		e.a.SseRR(0x66, 0x76, u, u, false)
		e.a.SseRR(0x66, 0xef, t, u, false)
	}
	e.shift(t, 2, 31)
	e.copy(out, t)
}

func (e *integerMapBodyEmitter) streamStart(stream int, dst Reg) int32 {
	base, delta := shareStream(&e.guard.plan.memory, stream)
	for i := uint8(0); i < e.addressN; i++ {
		if e.addressBase[i] == base {
			e.a.MovReg64(dst, e.addressReg[i])
			return delta
		}
	}
	e.a.Load64(dst, RSP, e.guard.slot(e.guard.startSlot(base)))
	return delta
}

// streamReadAddress returns an address that a memory operand can read without
// modifying it. Cached stream bases can be used directly instead of copying
// them into scratch; uncached bases are loaded into scratch as before.
func (e *integerMapBodyEmitter) streamReadAddress(stream int, scratch Reg) (Reg, int32) {
	base, delta := shareStream(&e.guard.plan.memory, stream)
	for i := uint8(0); i < e.addressN; i++ {
		if e.addressBase[i] == base {
			return e.addressReg[i], delta
		}
	}
	e.a.Load64(scratch, RSP, e.guard.slot(e.guard.startSlot(base)))
	return scratch, delta
}

func (e *integerMapBodyEmitter) load(id uint8, lanes uint8) {
	stream := int(e.recipe.loadStream[id])
	s := e.guard.plan.memory.streams[stream]
	addr, tmp := e.gp[1], e.gp[2]
	out := e.vector(id)
	disp := e.streamStart(stream, addr)
	switch s.stride {
	case 0:
		e.a.FLoadIdx(out, e.memoryBase, addr, disp, false)
		e.a.Pshufd(out, out, 0)
	case 4:
		if lanes == 1 {
			e.a.FLoadIdx(out, e.memoryBase, addr, disp, false)
		} else if lanes == 2 {
			e.a.FLoadIdx(out, e.memoryBase, addr, disp, true)
		} else {
			e.a.MovdquLoadIdx(out, e.memoryBase, addr, disp)
		}
	case 8:
		// A paired guarded read stream proves all four contiguous words safe.
		// Cache the full vector while its intervening stores are deferred.
		paired := false
		base, _ := shareStream(&e.guard.plan.memory, stream)
		if (lanes == 2 || lanes == 4) && e.guard.plan.packStores && (disp == 0 || disp == 4) {
			for other, peer := range e.guard.plan.memory.streams[:e.guard.plan.memory.streamN] {
				peerBase, peerDelta := shareStream(&e.guard.plan.memory, other)
				if peer.read && peerBase == base && peerDelta != disp && (peerDelta == 0 || peerDelta == 4) {
					paired = true
					break
				}
			}
		}
		if paired && lanes == 4 {
			first, second := e.fp[int(e.recipe.registers)-4], e.fp[int(e.recipe.registers)-3]
			if !e.cacheUsed[0] || !e.cacheUsed[1] || e.cacheBase[0] != base {
				e.a.MovdquLoadIdx(first, e.memoryBase, addr, 0)
				e.a.MovdquLoadIdx(second, e.memoryBase, addr, 16)
				e.cacheUsed = [2]bool{true, true}
				e.cacheBase[0] = base
			}
			shuffle := byte(0x88)
			if disp == 4 {
				shuffle = 0xdd
			}
			t, _ := e.scratch()
			e.a.Pshufd(out, first, shuffle)
			e.a.Pshufd(t, second, shuffle)
			e.a.SseRR(0x66, 0x6c, out, t, false)
			break
		}
		if paired {
			slot := -1
			for i := range e.cacheBase {
				if e.cacheUsed[i] && e.cacheBase[i] == base {
					slot = i
					break
				}
			}
			if slot < 0 {
				for i := range e.cacheBase {
					if !e.cacheUsed[i] {
						slot = i
						e.cacheUsed[i] = true
						e.cacheBase[i] = base
						e.a.MovdquLoadIdx(e.fp[int(e.recipe.registers)-4+i], e.memoryBase, addr, 0)
						break
					}
				}
			}
			if slot >= 0 {
				e.copy(out, e.fp[int(e.recipe.registers)-4+slot])
				if disp == 0 {
					e.a.Pshufd(out, out, 0x88)
				} else {
					e.a.Pshufd(out, out, 0xdd)
				}
				break
			}
		}
		e.a.FLoadIdx(out, e.memoryBase, addr, disp, false)
		for lane := byte(1); lane < lanes; lane++ {
			e.a.LoadIdx(tmp, e.memoryBase, addr, disp+int32(lane)*8, 4, false, false)
			e.a.Pinsrd(out, tmp, lane)
		}
	default:
		panic("unsupported integer map load stride")
	}
}
func (e *integerMapBodyEmitter) store(effect uint8, lanes uint8) {
	stream := int(e.guard.plan.memory.effectStream[effect])
	s := e.guard.plan.memory.streams[stream]
	addr, tmp := e.gp[1], e.gp[2]
	value := e.vector(e.plan.effects[effect].value)
	disp := e.streamStart(stream, addr)
	switch s.stride {
	case 4:
		if lanes == 1 {
			e.a.FStoreIdx(e.memoryBase, addr, value, disp, false)
		} else if lanes == 2 {
			e.a.FStoreIdx(e.memoryBase, addr, value, disp, true)
		} else {
			e.a.MovdquStoreIdx(e.memoryBase, addr, value, disp)
		}
	case 8:
		e.a.FStoreIdx(e.memoryBase, addr, value, disp, false)
		for lane := byte(1); lane < lanes; lane++ {
			e.a.Pextrd(tmp, value, lane)
			e.a.StoreIdx(e.memoryBase, addr, tmp, disp+int32(lane)*8, 4)
		}
	default:
		panic("unsupported integer map store stride")
	}
}

func (e *integerMapBodyEmitter) initialVectors() {
	p, r := e.plan, e.recipe
	// Entry locals and literals are immutable captured values. The induction
	// vector advances once per packet; other captures remain broadcasts.
	for id := uint8(1); id <= p.nodeN; id++ {
		if !r.used[id] {
			continue
		}
		n := p.nodes[id]
		out := e.vector(id)
		switch n.op {
		case 0x20:
			e.a.Load32(e.gp[2], RSP, e.guard.slot(int(n.bits)))
			e.a.MovGprToXmm(out, e.gp[2], false)
			if uint8(n.bits) == e.induction.local {
				for lane := byte(1); lane < e.guard.plan.lanes; lane++ {
					e.a.AluRI(0, e.gp[2], int32(e.induction.step), false)
					e.a.Pinsrd(out, e.gp[2], lane)
				}
			} else {
				e.a.Pshufd(out, out, 0)
			}
		case 0x41:
			e.broadcast(out, uint32(n.bits))
		}
	}
}

func (e *integerMapBodyEmitter) emit() {
	for i := uint8(0); i < e.addressN; i++ {
		e.a.Load64(e.addressReg[i], RSP, e.guard.slot(e.guard.startSlot(e.addressBase[i])))
	}
	if e.hasIncrement {
		e.broadcast(e.increment, e.induction.step*uint32(e.guard.plan.lanes))
	}
	if e.slp == nil {
		e.initialVectors()
	}
	e.a.Load32(e.gp[0], RSP, e.guard.slot(e.guard.countSlot()))
	e.a.AlignLoop()
	top := e.a.Len()
	e.a.AluRI(7, e.gp[0], int32(e.guard.plan.lanes), false)
	var scalarSites [integerMapMaxAliasPairs + 1]int
	siteN := 0
	scalarSites[siteN] = e.a.JccPlaceholder(condB)
	siteN++
	// Group writes in the same proven stream so a single conservative span
	// covers their overlap checks against a common invariant read.
	var grouped [integerMapMaxAliasPairs]bool
	for i, pair := range e.guard.plan.packetPairs[:e.guard.plan.packetN] {
		if grouped[i] {
			continue
		}
		base, delta := shareStream(&e.guard.plan.memory, int(pair.a))
		lo, hi := delta, delta+4
		stream := e.guard.plan.memory.streams[pair.a]
		for j := i + 1; j < int(e.guard.plan.packetN); j++ {
			other := e.guard.plan.packetPairs[j]
			otherBase, otherDelta := shareStream(&e.guard.plan.memory, int(other.a))
			if other.b != pair.b || otherBase != base || e.guard.plan.memory.streams[other.a].stride != stream.stride {
				continue
			}
			if otherDelta < lo {
				lo = otherDelta
			}
			if otherDelta+4 > hi {
				hi = otherDelta + 4
			}
			grouped[j] = true
		}
		start, _ := e.streamReadAddress(int(pair.a), e.gp[1])
		if lo != 0 {
			e.a.LeaDisp(e.gp[1], start, lo)
			start = e.gp[1]
		}
		other, d := e.streamReadAddress(int(pair.b), e.gp[3])
		if d != 0 {
			e.a.LeaDisp(e.gp[3], other, d)
			other = e.gp[3]
		}
		span := int64(e.guard.plan.lanes-1)*int64(stream.stride) + int64(hi) - int64(lo)
		if span > 0 && span <= 0x7ffffffc {
			// Both ranges have already passed widened bounds checks. For a
			// four-byte access, the unsigned distance (other+3)-start is
			// below span+3 exactly when the ranges overlap. Subtraction wraps
			// for disjoint addresses below start, making them compare above it.
			e.a.LeaDisp(e.gp[2], other, 3)
			e.a.AluRR(0x29, e.gp[2], start, true)
			e.a.AluRI(7, e.gp[2], int32(span+3), true)
			scalarSites[siteN] = e.a.JccPlaceholder(condB)
		} else {
			e.a.LeaDisp(e.gp[2], start, int32(span))
			e.a.Cmp64(e.gp[2], other)
			safe := e.a.JccPlaceholder(condBE)
			e.a.LeaDisp(e.gp[3], other, 4)
			e.a.Cmp64(e.gp[3], start)
			scalarSites[siteN] = e.a.JccPlaceholder(condA)
			e.a.PatchRel32(safe, e.a.Len())
		}
		siteN++
	}

	if e.slpWide {
		e.emitSLPWide()
		e.advance(4)
	} else if e.slp != nil {
		e.emitSLP()
		e.advance(2)
		e.emitSLP()
		e.advance(2)
	} else {
		e.emitPacket(e.guard.plan.lanes)
		e.advance(e.guard.plan.lanes)
	}
	e.a.AluRI(5, e.gp[0], int32(e.guard.plan.lanes), false)
	again := e.a.JccPlaceholder(condNE)
	e.a.PatchRel32(again, top)
	if e.slpWide {
		var extracted [integerMapMaxNodes + 1]bool
		for _, id := range e.slp.localNode {
			if id != 0 && !extracted[id] {
				reg := e.fp[e.slp.nodes[id].reg]
				e.a.YExtractI128(reg, reg, 1)
				extracted[id] = true
			}
		}
		e.a.VZeroUpper()
	}
	e.publish(e.guard.plan.lanes, e.slp != nil)
	done := e.a.JmpPlaceholder()
	for _, site := range scalarSites[:siteN] {
		e.a.PatchRel32(site, e.a.Len())
	}
	if e.slp != nil {
		if e.slpWide {
			e.a.VZeroUpper()
		}
		e.initialVectors()
	}
	e.emitPacket(1)
	e.advance(1)
	e.a.AluRI(5, e.gp[0], 1, false)
	again = e.a.JccPlaceholder(condNE)
	e.a.PatchRel32(again, top)
	e.publish(1, false)
	e.a.PatchRel32(done, e.a.Len())
}

func (e *integerMapBodyEmitter) emitPacket(lanes uint8) {
	p, r := e.plan, e.recipe
	e.cacheUsed = [2]bool{}
	var invariantStream uint8
	var invariantReg Reg
	invariantValid := false
	for _, event := range r.events[:r.eventN] {
		if event&integerMapStoreEvent != 0 {
			effect := uint8(event)
			if (lanes == 2 || lanes == 4) && e.guard.plan.packStores {
				if effect == e.guard.plan.packFirst {
					// This store is deferred. The packet guards prove that every
					// intervening read is unaffected by it.
					e.copy(e.fp[e.recipe.registers-5], e.vector(p.effects[effect].value))
					continue
				}
				if effect == e.guard.plan.packSecond {
					packed := e.fp[e.recipe.registers-5]
					if lanes == 4 {
						t, _ := e.scratch()
						e.copy(t, packed)
						e.a.SseRR(0x66, 0x6a, t, e.vector(p.effects[effect].value), false)
					}
					e.a.SseRR(0x66, 0x62, packed, e.vector(p.effects[effect].value), false)
					stream := int(e.guard.plan.memory.effectStream[e.guard.plan.packFirst])
					disp := e.streamStart(stream, e.gp[1])
					e.a.MovdquStoreIdx(e.memoryBase, e.gp[1], packed, disp)
					if lanes == 4 {
						t, _ := e.scratch()
						e.a.MovdquStoreIdx(e.memoryBase, e.gp[1], t, disp+16)
					}
					e.cacheUsed = [2]bool{}
					invariantValid = false
					continue
				}
			}
			e.cacheUsed = [2]bool{}
			invariantValid = false
			e.store(effect, lanes)
			continue
		}
		id := uint8(event)
		n := p.nodes[id]
		if r.skippedCompare[id] {
			continue
		}
		out := e.vector(id)
		if integerMapInvariantReuse && n.op == 0x28 {
			stream := r.loadStream[id]
			if e.guard.plan.memory.streams[stream].stride == 0 && invariantValid && stream == invariantStream && out == invariantReg {
				continue
			}
		}
		if invariantValid && out == invariantReg {
			invariantValid = false
		}
		switch {
		case n.op == 0x28:
			e.load(id, lanes)
			stream := r.loadStream[id]
			if integerMapInvariantReuse && e.guard.plan.memory.streams[stream].stride == 0 {
				invariantStream, invariantReg, invariantValid = stream, out, true
			}
		case n.op == 0x45:
			e.compare(out, e.vector(n.a), regNone, n.op)
		case n.op >= 0x46 && n.op <= 0x4f:
			e.compare(out, e.vector(n.a), e.vector(n.b), n.op)
		case n.op == 0x1b:
			if r.minSelect[id] {
				left, right := e.vector(n.a), e.vector(n.b)
				if out == right {
					left, right = right, left
				}
				e.copy(out, left)
				e.a.SseMapRR(0x66, 0x38, 0x39, out, right)
				continue
			}
			t, u := e.scratch()
			e.copy(t, e.vector(n.c))
			e.a.SseRR(0x66, 0xef, u, u, false)
			e.a.SseRR(0x66, 0x76, t, u, false)
			e.copy(out, e.vector(n.b))
			e.a.SseRR(0x66, 0xdb, out, t, false)
			e.a.SseRR(0x66, 0xdf, t, e.vector(n.a), false)
			e.a.SseRR(0x66, 0xeb, out, t, false)
		case n.op >= 0x74 && n.op <= 0x76:
			e.copy(out, e.vector(n.a))
			digit := byte(6)
			if n.op == 0x75 {
				digit = 4
			} else if n.op == 0x76 {
				digit = 2
			}
			e.shift(out, digit, byte(p.nodes[n.b].bits&31))
		default:
			e.copy(out, e.vector(n.a))
			right := e.vector(n.b)
			switch n.op {
			case 0x6a:
				e.a.SseRR(0x66, 0xfe, out, right, false)
			case 0x6b:
				e.a.SseRR(0x66, 0xfa, out, right, false)
			case 0x6c:
				e.a.SseMapRR(0x66, 0x38, 0x40, out, right)
			case 0x71:
				e.a.SseRR(0x66, 0xdb, out, right, false)
			case 0x72:
				e.a.SseRR(0x66, 0xeb, out, right, false)
			case 0x73:
				e.a.SseRR(0x66, 0xef, out, right, false)
			default:
				panic("unsupported integer map operation")
			}
		}
	}
}

func (e *integerMapBodyEmitter) advance(lanes uint8) {
	p, r := e.plan, e.recipe
	for i, s := range e.guard.plan.memory.streams[:e.guard.plan.memory.streamN] {
		base, _ := shareStream(&e.guard.plan.memory, i)
		if s.stride == 0 || base != i {
			continue
		}
		cached := false
		for j := uint8(0); j < e.addressN; j++ {
			if e.addressBase[j] == i {
				e.a.AluRI(0, e.addressReg[j], int32(s.stride*uint32(lanes)), true)
				cached = true
				break
			}
		}
		if cached {
			continue
		}
		e.a.Load64(e.gp[1], RSP, e.guard.slot(e.guard.startSlot(i)))
		e.a.AluRI(0, e.gp[1], int32(s.stride*uint32(lanes)), true)
		e.a.Store64(RSP, e.guard.slot(e.guard.startSlot(i)), e.gp[1])
	}
	index := p.locals[e.induction.local].entry
	if r.used[index] {
		t := e.increment
		if !e.hasIncrement || lanes != e.guard.plan.lanes {
			t, _ = e.scratch()
			e.broadcast(t, e.induction.step*uint32(lanes))
		}
		e.a.SseRR(0x66, 0xfe, e.vector(index), t, false)
	}
}

func (e *integerMapBodyEmitter) publish(lanes uint8, slp bool) {
	p := e.plan
	// Every changed local is published; the induction exit is calculated in
	// scalar i32 arithmetic and other outputs use the last original lane.
	for i, l := range p.locals[:p.localN] {
		if !l.written {
			continue
		}
		if i == int(e.induction.local) {
			e.a.Load32(e.gp[1], RSP, e.guard.slot(i))
			e.a.Load32(e.gp[2], RSP, e.guard.slot(e.guard.countSlot()))
			e.a.ImulRI(e.gp[2], int32(e.induction.step), false)
			e.a.Add32(e.gp[1], e.gp[2])
		} else if e.recipe.exitAffine[i] {
			a := e.facts[l.value]
			e.guard.affine(a, e.gp[1], e.gp[2])
			if c := a.coeff[e.induction.local]; c != 0 {
				e.a.Load32(e.gp[2], RSP, e.guard.slot(e.guard.countSlot()))
				e.a.AluRI(5, e.gp[2], 1, false)
				e.a.ImulRI(e.gp[2], int32(c*e.induction.step), false)
				e.a.Add32(e.gp[1], e.gp[2])
			}
		} else if slp {
			n := e.slp.localNode[i]
			e.a.Pextrd(e.gp[1], e.fp[e.slp.nodes[n].reg], e.slp.localLane[i])
		} else {
			e.a.Pextrd(e.gp[1], e.vector(l.value), lanes-1)
		}
		e.a.Store64(RSP, e.outputBase+int32(8*i), e.gp[1])
	}
}
