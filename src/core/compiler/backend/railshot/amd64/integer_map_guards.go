// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

// integerMapGuards describes nontrapping fast-path eligibility checks. Failure
// must transfer to the original checked loop before any vector effect occurs.
// Dynamic address arithmetic remains modular i32; memory immediates are added
// separately in widened arithmetic by the eventual emitter.
type integerMapGuards struct {
	localN                uint8
	counter               uint8
	lanes                 uint8
	step                  uint32
	minTrip               uint32
	limit                 integerMapAffine
	memory                integerMapMemoryPlan
	packetPairs           [integerMapMaxAliasPairs]integerMapAliasPair
	packetN               uint8
	packStores            bool
	packFirst, packSecond uint8
}

func (p *integerMapPlan) guardPlan(facts *[integerMapMaxNodes + 1]integerMapAffine, ind integerMapInduction, used *[integerMapMaxNodes + 1]bool) (integerMapGuards, bool) {
	// A post-test != latch with a positive power-of-two update admits a cheap
	// exact trip-count check. Other predicates remain candidates for later plans.
	if ind.predicate != 0x47 || ind.step == 0 || ind.step >= 1<<31 || ind.step&(ind.step-1) != 0 {
		return integerMapGuards{}, false
	}
	if int(ind.local) >= int(p.localN) || int(ind.limit) > int(p.nodeN) || !facts[ind.limit].valid {
		return integerMapGuards{}, false
	}
	memory, ok := p.memoryPlan(facts, ind, used)
	if !ok {
		return integerMapGuards{}, false
	}
	g := integerMapGuards{localN: p.localN, counter: ind.local, lanes: 4, step: ind.step, limit: facts[ind.limit], memory: memory}
	// An invariant load may overlap a destination somewhere in the loop.
	// Check that relationship per packet instead of rejecting the whole loop;
	// overlapping packets execute one source iteration in order.
	g.memory.pairN = 0
	for _, pair := range memory.pairs[:memory.pairN] {
		a, b := memory.streams[pair.a], memory.streams[pair.b]
		if a.write && b.read && !b.write && b.stride == 0 {
			g.packetPairs[g.packetN] = pair
			g.packetN++
		} else {
			g.memory.pairs[g.memory.pairN] = pair
			g.memory.pairN++
		}
	}
	if integerMapPackEnabled {
		g.selectAdjacentStores(p)
	}
	return g, true
}

// selectAdjacentStores groups adjacent word streams only when every intervening
// read is either statically disjoint or covered by a packet/full-range guard.
func (g *integerMapGuards) selectAdjacentStores(p *integerMapPlan) {
	var stores [2]uint8
	n := 0
	for i, e := range p.effects[:p.effectN] {
		if e.op != 0x36 {
			continue
		}
		if n == len(stores) {
			return
		}
		stores[n] = uint8(i)
		n++
	}
	if n != 2 {
		return
	}
	a, b := int(g.memory.effectStream[stores[0]]), int(g.memory.effectStream[stores[1]])
	as, bs := g.memory.streams[a], g.memory.streams[b]
	baseA, deltaA := shareStream(&g.memory, a)
	baseB, deltaB := shareStream(&g.memory, b)
	if as.stride != 8 || bs.stride != 8 || baseA != baseB || deltaA != 0 || deltaB != 4 {
		return
	}
	for i := int(stores[0]) + 1; i < int(stores[1]); i++ {
		if p.effects[i].op != 0x28 {
			return
		}
		j := int(g.memory.effectStream[i])
		if j == a {
			return
		}
		other := g.memory.streams[j]
		if disjointWordResidues(as, other) {
			continue
		}
		guarded := false
		for _, pair := range g.memory.pairs[:g.memory.pairN] {
			if int(pair.a) == a && int(pair.b) == j {
				guarded = true
			}
		}
		for _, pair := range g.packetPairs[:g.packetN] {
			if int(pair.a) == a && int(pair.b) == j {
				guarded = true
			}
		}
		if !guarded {
			return
		}
	}
	g.packStores, g.packFirst, g.packSecond = true, stores[0], stores[1]
}
