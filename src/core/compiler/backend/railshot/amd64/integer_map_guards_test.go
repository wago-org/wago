// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type integerGuardRange struct{ start, end uint64 }

// Reference model for the required nontrapping runtime checks. It is kept in
// tests so that emitted native guards can later be checked against this model.
func evaluateIntegerMapGuards(g integerMapGuards, locals [integerMapMaxLocals]uint32, memoryBytes uint64) (uint32, bool) {
	evaluate := func(a integerMapAffine) uint32 {
		x := a.constant
		for i, c := range a.coeff {
			x += c * locals[i]
		}
		return x
	}
	counter, limit := locals[g.counter], evaluate(g.limit)
	if limit <= counter {
		return 0, false
	}
	delta := limit - counter
	if delta&(g.step-1) != 0 {
		return 0, false
	}
	count := delta / g.step
	if count == 0 || count < g.minTrip {
		return 0, false
	}
	var ranges [integerMapMaxEffects]integerGuardRange
	for i, s := range g.memory.streams[:g.memory.streamN] {
		start := uint64(evaluate(s.address))
		end := start + uint64(count-1)*uint64(s.stride) + 4
		if end > 1<<32 {
			return 0, false
		}
		start += s.offset
		end += s.offset
		if end > memoryBytes {
			return 0, false
		}
		ranges[i] = integerGuardRange{start, end}
	}
	for _, pair := range g.memory.pairs[:g.memory.pairN] {
		a, b := ranges[pair.a], ranges[pair.b]
		sa, sb := g.memory.streams[pair.a], g.memory.streams[pair.b]
		if sa.stride != 0 && sa.stride == sb.stride && a.start == b.start {
			continue
		}
		if a.start < b.end && b.start < a.end {
			return 0, false
		}
	}
	return count, true
}

func integerMapGuardFixture(t *testing.T) (integerMapPlan, integerMapGuards) {
	t.Helper()
	body := []byte{0x20, 0, 0x20, 1, 0x6a, 0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0, 0x36, 2, 0, 0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b}
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}}
	var p integerMapPlan
	if !inspectIntegerMapLoop(wasm.ReaderFrom(body), []machineType{mtI32, mtI32, mtI32, mtI32}, wasm.NewModuleInstructionClassifier(m, true), &p) {
		t.Fatal("capture rejected")
	}
	facts := p.affineFacts()
	ind, ok := p.induction(&facts)
	if !ok {
		t.Fatal("induction rejected")
	}
	used := p.requiredNodes()
	guards, ok := p.guardPlan(&facts, ind, &used)
	if !ok {
		t.Fatal("guards rejected")
	}
	return p, guards
}

func TestIntegerMapGuardsFallbackConditions(t *testing.T) {
	p, g := integerMapGuardFixture(t)
	makeLocals := func(dst, counter, src, limit uint32) (out [integerMapMaxLocals]uint32) {
		for i, l := range p.locals[:p.localN] {
			switch l.index {
			case 0:
				out[i] = dst
			case 1:
				out[i] = counter
			case 2:
				out[i] = src
			case 3:
				out[i] = limit
			}
		}
		return
	}
	cases := []struct {
		name                     string
		dst, counter, src, limit uint32
		memory                   uint64
		count                    uint32
		ok                       bool
	}{
		{"disjoint", 0, 0, 128, 16, 65536, 4, true},
		{"exact alias", 0, 0, 0, 16, 65536, 4, true},
		{"partial alias", 0, 0, 8, 16, 65536, 0, false},
		{"short remainder", 0, 0, 128, 12, 65536, 3, true},
		{"non-divisible step", 0, 0, 128, 15, 65536, 0, false},
		{"zero trip wraps", 0, 0, 128, 0, 65536, 0, false},
		{"counter wraps", 0, 0xfffffffc, 128, 4, 1 << 32, 0, false},
		{"memory end", 65524, 0, 128, 16, 65536, 0, false},
		{"dynamic address wraps", 0xfffffff4, 0, 128, 16, 1 << 32, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := evaluateIntegerMapGuards(g, makeLocals(tc.dst, tc.counter, tc.src, tc.limit), tc.memory)
			if n != tc.count || ok != tc.ok {
				t.Fatalf("count=%d ok=%v", n, ok)
			}
		})
	}
}

func TestIntegerMapGuardAcceptedRangesMatchEveryLane(t *testing.T) {
	p, g := integerMapGuardFixture(t)
	rng := rand.New(rand.NewSource(891))
	for test := 0; test < 10000; test++ {
		var locals [integerMapMaxLocals]uint32
		for i, l := range p.locals[:p.localN] {
			switch l.index {
			case 0, 2:
				locals[i] = rng.Uint32()
			case 1:
				locals[i] = rng.Uint32() & 255
			case 3:
				locals[i] = rng.Uint32() & 511
			}
		}
		if test%2 == 0 {
			for i, l := range p.locals[:p.localN] {
				if l.index == 0 {
					locals[i] &= 255
				}
				if l.index == 2 {
					locals[i] = (locals[i] & 255) + 512
				}
				if l.index == 1 {
					locals[i] = 0
				}
				if l.index == 3 {
					locals[i] = uint32(16 * (1 + rng.Intn(8)))
				}
			}
		}
		memory := uint64(4096)
		count, ok := evaluateIntegerMapGuards(g, locals, memory)
		if !ok {
			continue
		}
		facts := p.affineFacts()
		address := func(effect integerMapEffect, lane uint32) uint64 {
			a := facts[effect.address]
			addr := a.constant
			for j, c := range a.coeff {
				v := locals[j]
				if uint8(j) == g.counter {
					v += lane * g.step
				}
				addr += c * v
			}
			return uint64(addr) + effect.offset
		}
		for i := uint32(0); i < count; i++ {
			for _, effect := range p.effects[:p.effectN] {
				addr := address(effect, i)
				if addr+4 > memory {
					t.Fatalf("accepted out-of-bounds lane %d", i)
				}
				for j := uint32(0); j < count; j++ {
					if i == j {
						continue
					}
					for _, other := range p.effects[:p.effectN] {
						if effect.op != 0x36 && other.op != 0x36 {
							continue
						}
						b := address(other, j)
						if addr < b+4 && b < addr+4 {
							t.Fatalf("accepted loop-carried overlap: lanes %d and %d", i, j)
						}
					}
				}
			}
		}
	}
}

func TestIntegerMapGuardPlanRejectsUnsupportedTrips(t *testing.T) {
	p, _ := integerMapGuardFixture(t)
	facts := p.affineFacts()
	ind, ok := p.induction(&facts)
	if !ok {
		t.Fatal("induction missing")
	}
	used := p.requiredNodes()
	for _, tc := range []struct {
		name   string
		change func(*integerMapInduction)
	}{
		{"zero step", func(x *integerMapInduction) { x.step = 0 }},
		{"non-power-of-two step", func(x *integerMapInduction) { x.step = 3 }},
		{"descending step", func(x *integerMapInduction) { x.step = 0xfffffffc }},
		{"unsupported predicate", func(x *integerMapInduction) { x.predicate = 0x49 }},
		{"invalid counter", func(x *integerMapInduction) { x.local = p.localN }},
		{"invalid limit", func(x *integerMapInduction) { x.limit = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := ind
			tc.change(&candidate)
			if _, ok := p.guardPlan(&facts, candidate, &used); ok {
				t.Fatal("unsupported guard plan accepted")
			}
		})
	}
}
