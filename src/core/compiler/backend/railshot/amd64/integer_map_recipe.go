// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

const (
	integerMapNoRegister uint8 = 0xff
	integerMapStoreEvent       = 0x100
	integerMapMaxEvents        = integerMapMaxNodes + integerMapMaxEffects
)

// integerMapRecipe is a bounded schedule and scratch assignment for a single
// candidate. Register indices refer only to caller-leased vector scratch; they
// are not allocator identities and never survive candidate emission.
type integerMapRecipe struct {
	used                           [integerMapMaxNodes + 1]bool
	exitAffine                     [integerMapMaxLocals]bool
	minSelect, skippedCompare      [integerMapMaxNodes + 1]bool
	reg                            [integerMapMaxNodes + 1]uint8
	loadStream                     [integerMapMaxNodes + 1]uint8
	events                         [integerMapMaxEvents]uint16
	eventN, registers              uint8
	needsSSE41, needsAVX2, strided bool
}

// buildIntegerMapRecipe retains all stores and changed locals. Loads discarded
// by source may be omitted only after the separate guard proves every original
// memory access safe. No machine state is changed by this bounded selection.
func (p *integerMapPlan) buildIntegerMapRecipe(ind integerMapInduction, memory *integerMapMemoryPlan, facts *[integerMapMaxNodes + 1]integerMapAffine) (r integerMapRecipe, ok bool) {
	for i := range r.reg {
		r.reg[i] = integerMapNoRegister
	}
	for _, e := range p.effects[:p.effectN] {
		if e.op == 0x36 {
			r.used[e.value] = true
		}
	}
	for i, l := range p.locals[:p.localN] {
		if l.written && i != int(ind.local) {
			a := facts[l.value]
			valid := integerMapAffineExit && a.valid
			for j, c := range a.coeff {
				if c != 0 && j != int(ind.local) && p.locals[j].written {
					valid = false
				}
			}
			r.exitAffine[i] = valid
			if !valid {
				r.used[l.value] = true
			}
		}
	}
	for i := int(p.nodeN); i > 0; i-- {
		if !r.used[i] {
			continue
		}
		n := p.nodes[i]
		switch {
		case n.op == 0x20 || n.op == 0x41 || n.op == 0x28:
		case n.op == 0x45:
			r.used[n.a] = true
		case n.op == 0x1b:
			r.used[n.a], r.used[n.b], r.used[n.c] = true, true, true
		case n.op >= 0x46 && n.op <= 0x4f || n.op >= 0x6a && n.op <= 0x6c || n.op >= 0x71 && n.op <= 0x76:
			r.used[n.a], r.used[n.b] = true, true
			if n.op == 0x6c {
				r.needsSSE41 = true
			}
			if n.op >= 0x74 && n.op <= 0x76 && p.nodes[n.b].op != 0x41 {
				r.needsAVX2 = true
			}
		default:
			return integerMapRecipe{}, false
		}
	}
	r.used[0] = false
	var uses [integerMapMaxNodes + 1]uint8
	addUse := func(id uint8) bool {
		if id == 0 || !r.used[id] || uses[id] == 255 {
			return false
		}
		uses[id]++
		return true
	}
	for _, e := range p.effects[:p.effectN] {
		if e.op == 0x36 && !addUse(e.value) {
			return integerMapRecipe{}, false
		}
	}
	for i, l := range p.locals[:p.localN] {
		if l.written && i != int(ind.local) && !r.exitAffine[i] && !addUse(l.value) {
			return integerMapRecipe{}, false
		}
	}
	for i := 1; i <= int(p.nodeN); i++ {
		if !r.used[i] {
			continue
		}
		n := p.nodes[i]
		switch {
		case n.op == 0x45:
			if !addUse(n.a) {
				return integerMapRecipe{}, false
			}
		case n.op == 0x1b:
			if !addUse(n.a) || !addUse(n.b) || !addUse(n.c) {
				return integerMapRecipe{}, false
			}
		case n.op >= 0x46 && n.op <= 0x4f || n.op >= 0x6a && n.op <= 0x6c || n.op >= 0x71 && n.op <= 0x76:
			if !addUse(n.a) || !addUse(n.b) {
				return integerMapRecipe{}, false
			}
		}
	}
	// A signed min is an exact selection of its two operands. Preserve the
	// comparison when it has another consumer or is a required local output.
	for i := 1; i <= int(p.nodeN); i++ {
		n := p.nodes[i]
		if !r.used[i] || n.op != 0x1b || n.c == 0 || uses[n.c] != 1 {
			continue
		}
		c := p.nodes[n.c]
		if c.op == 0x4a && n.a == c.b && n.b == c.a || c.op == 0x48 && n.a == c.a && n.b == c.b {
			r.minSelect[i] = true
			r.skippedCompare[n.c] = true
		}
	}
	for i, e := range p.effects[:p.effectN] {
		if e.op == 0x28 && r.used[e.value] {
			r.loadStream[e.value] = memory.effectStream[i]
			stride := memory.streams[memory.effectStream[i]].stride
			if stride != 0 && stride != 4 {
				r.strided = true
			}
		}
		if e.op == 0x36 && memory.streams[memory.effectStream[i]].stride != 4 {
			r.strided = true
		}
	}
	// Two registers remain available to each recipe for masks, shuffles, and
	// target instructions with extra operands. Peak pressure is bounded before
	// any native state or allocator ownership is changed.
	const maxValues = 14
	var occupied [maxValues]bool
	alloc := func() uint8 {
		for i := range occupied {
			if !occupied[i] {
				occupied[i] = true
				return uint8(i)
			}
		}
		return integerMapNoRegister
	}
	free := func(id uint8, keep uint8) {
		uses[id]--
		if uses[id] == 0 && r.reg[id] != keep && p.nodes[id].op != 0x20 && p.nodes[id].op != 0x41 {
			occupied[r.reg[id]] = false
		}
	}
	high := uint8(0)
	note := func(reg uint8) {
		if reg+1 > high {
			high = reg + 1
		}
	}
	for i := 1; i <= int(p.nodeN); i++ {
		if r.used[i] && (p.nodes[i].op == 0x20 || p.nodes[i].op == 0x41) {
			reg := alloc()
			if reg == integerMapNoRegister {
				return integerMapRecipe{}, false
			}
			r.reg[i] = reg
			note(reg)
		}
	}
	var stores [integerMapMaxEffects]uint8
	storeN := 0
	for i, e := range p.effects[:p.effectN] {
		if e.op == 0x36 {
			stores[storeN] = uint8(i)
			storeN++
		}
	}
	nextNode, nextStore := 1, 0
	for {
		for nextNode <= int(p.nodeN) && (!r.used[nextNode] || p.nodes[nextNode].op == 0x20 || p.nodes[nextNode].op == 0x41) {
			nextNode++
		}
		if nextNode > int(p.nodeN) && nextStore == storeN {
			break
		}
		if int(r.eventN) == len(r.events) {
			return integerMapRecipe{}, false
		}
		if nextStore < storeN && (nextNode > int(p.nodeN) || p.effects[stores[nextStore]].pc < p.nodes[nextNode].pc) {
			effect := stores[nextStore]
			nextStore++
			r.events[r.eventN] = integerMapStoreEvent | uint16(effect)
			r.eventN++
			free(p.effects[effect].value, integerMapNoRegister)
			continue
		}
		id := uint8(nextNode)
		nextNode++
		n := p.nodes[id]
		reg := integerMapNoRegister
		if n.op != 0x1b && n.op != 0x28 && uses[n.a] == 1 && p.nodes[n.a].op != 0x20 && p.nodes[n.a].op != 0x41 {
			reg = r.reg[n.a]
		}
		if reg == integerMapNoRegister {
			reg = alloc()
		}
		if reg == integerMapNoRegister {
			return integerMapRecipe{}, false
		}
		r.reg[id] = reg
		note(reg)
		switch {
		case n.op == 0x45:
			free(n.a, reg)
		case n.op == 0x1b:
			free(n.a, reg)
			free(n.b, reg)
			free(n.c, reg)
		case n.op >= 0x46 && n.op <= 0x4f || n.op >= 0x6a && n.op <= 0x6c || n.op >= 0x71 && n.op <= 0x76:
			free(n.a, reg)
			free(n.b, reg)
		}
		r.events[r.eventN] = uint16(id)
		r.eventN++
	}
	r.registers = high + 2
	return r, true
}
