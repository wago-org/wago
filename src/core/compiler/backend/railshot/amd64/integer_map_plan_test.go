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

func TestIntegerMapCapturesMutableLocalsAndEffects(t *testing.T) {
	body := []byte{
		0x20, 0, // output address
		0x20, 1, // capture old local before the update
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1,
		0x20, 2, 0x28, 2, 4, // load at a widened immediate
		0x6a, 0x36, 2, 8, // store old local plus loaded value
		0x20, 1, 0x20, 3, 0x49, 0x0d, 0, 0x0b,
	}
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}}
	r := wasm.ReaderFrom(body)
	var p integerMapPlan
	if !inspectIntegerMapLoop(r, []machineType{mtI32, mtI32, mtI32, mtI32}, wasm.NewModuleInstructionClassifier(m, true), &p) {
		t.Fatal("candidate rejected")
	}
	if r.Offset() != 0 || p.endPC != len(body) || p.effectN != 2 || p.effects[0].op != 0x28 || p.effects[0].offset != 4 || p.effects[1].op != 0x36 || p.effects[1].offset != 8 {
		t.Fatalf("reader or effects: offset=%d plan=%+v", r.Offset(), p)
	}
	var cell integerMapLocal
	for _, l := range p.locals[:p.localN] {
		if l.index == 1 {
			cell = l
		}
	}
	if !cell.written || cell.entry == cell.value {
		t.Fatal("updated local was not captured")
	}
	sum := p.nodes[p.effects[1].value]
	if sum.op != 0x6a || sum.a != cell.entry || sum.b != p.effects[0].value {
		t.Fatal("store did not retain the old local and load", sum, cell)
	}
	if p.nodes[p.predicate].a != cell.value {
		t.Fatal("predicate lost the final local value")
	}
}

func TestIntegerMapRejectsUnsupportedAndOverBudget(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	valid := []byte{0x20, 0, 0x28, 2, 0, 0x1a, 0x41, 1, 0x0d, 0, 0x0b}
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"unused trapping load is ordered", valid, true},
		{"call barrier", append([]byte{0x10, 0}, valid...), false},
		{"division barrier", append([]byte{0x41, 1, 0x41, 1, 0x6d, 0x1a}, valid...), false},
		{"outer branch", []byte{0x20, 0, 0x28, 2, 0, 0x1a, 0x41, 1, 0x0d, 1, 0x0b}, false},
		{"truncated memory immediate", []byte{0x20, 0, 0x28, 0x80}, false},
		{"operation budget", append(make([]byte, integerMapMaxOps), valid...), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "operation budget" {
				for i := 0; i < integerMapMaxOps; i++ {
					tc.body[i] = 0x01
				}
			}
			r := wasm.ReaderFrom(tc.body)
			var p integerMapPlan
			if got := inspectIntegerMapLoop(r, []machineType{mtI32}, classifier, &p); got != tc.want || r.Offset() != 0 {
				t.Fatalf("got=%v want=%v offset=%d", got, tc.want, r.Offset())
			}
			if tc.want && (p.effectN != 1 || p.effects[0].op != 0x28) {
				t.Fatal("unused load was not retained")
			}
		})
	}
}

func TestIntegerMapAffineStrideAndUnknownCarriedValue(t *testing.T) {
	// dst + 4*i, followed by i += 1; the local value itself remains mutable.
	body := []byte{0x20, 0, 0x20, 1, 0x41, 2, 0x74, 0x6a, 0x20, 2, 0x28, 2, 0, 0x36, 2, 0, 0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b}
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}}
	var p integerMapPlan
	if !inspectIntegerMapLoop(wasm.ReaderFrom(body), []machineType{mtI32, mtI32, mtI32, mtI32}, wasm.NewModuleInstructionClassifier(m, true), &p) {
		t.Fatal("candidate rejected")
	}
	facts := p.affineFacts()
	if stride, ok := p.addressStride(&facts, p.effects[1].address); !ok || stride != 4 {
		t.Fatalf("stride=%d ok=%v", stride, ok)
	}
	// A loaded value carried into the next iteration makes the same address
	// recurrence unknown, even though one iteration's expression is affine.
	for i := range p.locals[:p.localN] {
		if p.locals[i].index == 1 {
			p.locals[i].value = p.effects[0].value
		}
	}
	facts = p.affineFacts()
	if stride, ok := p.addressStride(&facts, p.effects[1].address); ok {
		t.Fatalf("accepted unknown carried value: stride=%d", stride)
	}
}

func TestIntegerMapInductionIsCandidateNotTripProof(t *testing.T) {
	body := []byte{0x20, 0, 0x20, 1, 0x41, 2, 0x74, 0x6a, 0x20, 2, 0x28, 2, 0, 0x36, 2, 0, 0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b}
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}}
	var p integerMapPlan
	if !inspectIntegerMapLoop(wasm.ReaderFrom(body), []machineType{mtI32, mtI32, mtI32, mtI32}, wasm.NewModuleInstructionClassifier(m, true), &p) {
		t.Fatal("candidate rejected")
	}
	facts := p.affineFacts()
	ind, ok := p.induction(&facts)
	if !ok || p.locals[ind.local].index != 1 || ind.step != 1 || ind.predicate != 0x47 || ind.reversed {
		t.Fatalf("induction=%+v ok=%v", ind, ok)
	}
	// A limit written in the iteration is not an invariant bound.
	for i := range p.locals[:p.localN] {
		if p.locals[i].index == 3 {
			p.locals[i].written = true
		}
	}
	if _, ok = p.induction(&facts); ok {
		t.Fatal("accepted a written bound")
	}
}

// Compare the affine summary with independent modular evaluation of many
// captured arithmetic trees, including wrapped constants and shift counts.
func TestIntegerMapAffineFactsMatchModularEvaluation(t *testing.T) {
	rng := rand.New(rand.NewSource(731))
	for sample := 0; sample < 200; sample++ {
		var p integerMapPlan
		for i := 0; i < 4; i++ {
			if _, ok := p.local(uint32(i)); !ok {
				t.Fatal("local capacity")
			}
		}
		var values [integerMapMaxNodes + 1]uint32
		for i := 0; i < 4; i++ {
			values[p.locals[i].entry] = rng.Uint32()
		}
		entry := values
		for j := 0; j < 70; j++ {
			a := uint8(1 + rng.Intn(int(p.nodeN)))
			var n integerMapNode
			switch rng.Intn(4) {
			case 0:
				n = integerMapNode{op: 0x41, bits: uint64(rng.Uint32())}
			case 1:
				n = integerMapNode{op: 0x6a, a: a, b: uint8(1 + rng.Intn(int(p.nodeN)))}
			case 2:
				n = integerMapNode{op: 0x6b, a: a, b: uint8(1 + rng.Intn(int(p.nodeN)))}
			case 3:
				constant := p.add(integerMapNode{op: 0x41, bits: uint64(rng.Uint32())})
				values[constant] = uint32(p.nodes[constant].bits)
				if rng.Intn(2) == 0 {
					n = integerMapNode{op: 0x6c, a: a, b: constant}
				} else {
					n = integerMapNode{op: 0x74, a: a, b: constant}
				}
			}
			id := p.add(n)
			if id == 0 {
				t.Fatal("node capacity")
			}
			switch n.op {
			case 0x41:
				values[id] = uint32(n.bits)
			case 0x6a:
				values[id] = values[n.a] + values[n.b]
			case 0x6b:
				values[id] = values[n.a] - values[n.b]
			case 0x6c:
				values[id] = values[n.a] * values[n.b]
			case 0x74:
				values[id] = values[n.a] << (values[n.b] & 31)
			}
		}
		facts := p.affineFacts()
		for i := 1; i <= int(p.nodeN); i++ {
			a := facts[i]
			if !a.valid {
				continue
			}
			got := a.constant
			for j, c := range a.coeff {
				if j < int(p.localN) {
					got += c * entry[p.locals[j].entry]
				}
			}
			if got != values[i] {
				t.Fatalf("sample=%d node=%d got=%x want=%x", sample, i, got, values[i])
			}
		}
	}
}

func TestIntegerMapRequiredOutputsAndCarriedInputs(t *testing.T) {
	// The output local is overwritten before use, while a separate old value is
	// explicitly used in the loop. The former is safe; the latter is carried.
	body := []byte{0x20, 0, 0x20, 1, 0x28, 2, 0, 0x22, 2, 0x36, 2, 0, 0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b}
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}}
	var p integerMapPlan
	if !inspectIntegerMapLoop(wasm.ReaderFrom(body), []machineType{mtI32, mtI32, mtI32, mtI32}, wasm.NewModuleInstructionClassifier(m, true), &p) {
		t.Fatal("candidate rejected")
	}
	facts := p.affineFacts()
	ind, ok := p.induction(&facts)
	if !ok {
		t.Fatal("induction missing")
	}
	used := p.requiredNodes()
	if !p.independentInputs(ind, &used) {
		t.Fatal("overwrite-before-read classified as carried")
	}
	var temp integerMapLocal
	for _, l := range p.locals[:p.localN] {
		if l.index == 2 {
			temp = l
		}
	}
	if !used[temp.value] || used[temp.entry] {
		t.Fatal("incorrect output roots")
	}
	// A store consuming the original temporary observes carried state.
	p.effects[1].value = temp.entry
	used = p.requiredNodes()
	if p.independentInputs(ind, &used) {
		t.Fatal("accepted a required carried value")
	}
}

func TestIntegerMapMemoryPlanRequiresAliasProof(t *testing.T) {
	// Two independently based streams advance by one word. They require a
	// runtime disjointness proof; their Wasm local indices do not prove aliasing.
	body := []byte{0x20, 0, 0x20, 1, 0x6a, 0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0, 0x36, 2, 0, 0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b}
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}}
	var p integerMapPlan
	if !inspectIntegerMapLoop(wasm.ReaderFrom(body), []machineType{mtI32, mtI32, mtI32, mtI32}, wasm.NewModuleInstructionClassifier(m, true), &p) {
		t.Fatal("candidate rejected")
	}
	facts := p.affineFacts()
	ind, ok := p.induction(&facts)
	if !ok {
		t.Fatal("induction missing")
	}
	used := p.requiredNodes()
	plan, ok := p.memoryPlan(&facts, ind, &used)
	if !ok || plan.streamN != 2 || plan.pairN != 1 || plan.streams[0].stride != 4 || plan.streams[1].stride != 4 {
		t.Fatalf("plan=%+v ok=%v", plan, ok)
	}
	// Exact same moving stream is independent across lanes once bounds and
	// non-wrapping range checks have succeeded.
	p.effects[1].address = p.effects[0].address
	plan, ok = p.memoryPlan(&facts, ind, &used)
	if !ok || plan.streamN != 1 || plan.pairN != 0 || !plan.streams[0].read || !plan.streams[0].write {
		t.Fatalf("same stream=%+v ok=%v", plan, ok)
	}
	// An invariant store is a recurrence and cannot use this map plan.
	p.effects[1].address = p.locals[0].entry
	if _, ok = p.memoryPlan(&facts, ind, &used); ok {
		t.Fatal("accepted an invariant store")
	}
}

func TestIntegerMapInterleavedWordResidues(t *testing.T) {
	a := integerMapStream{address: integerMapAffine{valid: true}, stride: 8}
	b := a
	b.offset = 4
	if !disjointWordResidues(a, b) || !disjointWordResidues(b, a) {
		t.Fatal("complementary word lanes not proved disjoint")
	}
	b.offset = 3
	if disjointWordResidues(a, b) {
		t.Fatal("overlapping words proved disjoint")
	}
	b.offset = 4
	b.address.coeff[1] = 1
	if disjointWordResidues(a, b) {
		t.Fatal("independent bases proved disjoint")
	}
	b = a
	b.offset = 4
	b.stride = 12
	if disjointWordResidues(a, b) {
		t.Fatal("mismatched strides proved disjoint")
	}
}

func TestIntegerMapInterleaveProofAcrossI32Wrap(t *testing.T) {
	for _, stride := range []uint32{8, 16, 32} {
		for ca := uint32(0); ca < stride; ca++ {
			for cb := uint32(0); cb < stride; cb++ {
				for _, oa := range []uint64{0, 3, 1 << 32} {
					for _, ob := range []uint64{0, 5, 1 << 32} {
						a := integerMapStream{address: integerMapAffine{valid: true, constant: ca}, offset: oa, stride: stride}
						b := integerMapStream{address: integerMapAffine{valid: true, constant: cb}, offset: ob, stride: stride}
						if !disjointWordResidues(a, b) {
							continue
						}
						for _, base := range []uint32{0, 7, 0xffffffe0, 0xfffffff9} {
							for i := uint32(0); i < 8; i++ {
								for j := uint32(0); j < 8; j++ {
									x := uint64(base+ca+i*stride) + oa
									y := uint64(base+cb+j*stride) + ob
									if x < y+4 && y < x+4 {
										t.Fatalf("stride=%d ca=%d cb=%d oa=%d ob=%d base=%x i=%d j=%d", stride, ca, cb, oa, ob, base, i, j)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}
