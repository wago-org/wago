// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

const (
	integerMapMaxOps     = 96
	integerMapMaxNodes   = 128
	integerMapMaxLocals  = 16
	integerMapMaxEffects = 32
	integerMapMaxBytes   = 512
)

// integerMapPlan owns one candidate iteration. It describes source semantics,
// not physical locations; selecting a vector recipe requires separate proofs.
// Node zero is invalid, so a missing operand cannot be mistaken for a value.
type integerMapNode struct {
	bits        uint64 // constant bits, local slot, or widened load immediate
	pc          uint32
	op, a, b, c uint8
}
type integerMapLocal struct {
	index        uint32
	entry, value uint8
	written      bool
}
type integerMapEffect struct {
	offset             uint64
	pc                 uint32
	op, address, value uint8
}
type integerMapPlan struct {
	nodes                  [integerMapMaxNodes + 1]integerMapNode
	locals                 [integerMapMaxLocals]integerMapLocal
	effects                [integerMapMaxEffects]integerMapEffect
	nodeN, localN, effectN uint8
	predicate              uint8
	endPC                  int
}

func (p *integerMapPlan) add(n integerMapNode) uint8 {
	if int(p.nodeN) == integerMapMaxNodes {
		return 0
	}
	p.nodeN++
	p.nodes[p.nodeN] = n
	return p.nodeN
}
func (p *integerMapPlan) local(index uint32) (uint8, bool) {
	for i := uint8(0); i < p.localN; i++ {
		if p.locals[i].index == index {
			return i, true
		}
	}
	if int(p.localN) == integerMapMaxLocals {
		return 0, false
	}
	i := p.localN
	id := p.add(integerMapNode{op: 0x20, bits: uint64(i)})
	if id == 0 {
		return 0, false
	}
	p.locals[i] = integerMapLocal{index: index, entry: id, value: id}
	p.localN++
	return i, true
}
func (p *integerMapPlan) effect(e integerMapEffect) bool {
	if int(p.effectN) == integerMapMaxEffects {
		return false
	}
	p.effects[p.effectN] = e
	p.effectN++
	return true
}

// inspectIntegerMapLoop looks only at one flat, void loop body. It never
// changes the caller's reader. Unsupported effects or exhausted capacities
// reject the candidate; capture alone does not permit reordering or emission.
func inspectIntegerMapLoop(source wasm.Reader, types []machineType, classifier wasm.ModuleInstructionClassifier, p *integerMapPlan) bool {
	*p = integerMapPlan{}
	start := source.Offset()
	size := source.BytesLeft()
	if size > integerMapMaxBytes {
		size = integerMapMaxBytes
	}
	data, err := source.Bytes(size)
	if err != nil {
		return false
	}
	r := wasm.ReaderFrom(data)
	var stack [32]uint8
	depth := 0
	for count := 0; count < integerMapMaxOps && r.HasNext(); count++ {
		pc := uint32(start + r.Offset())
		op, err := r.Byte()
		if err != nil {
			return false
		}
		var id uint8
		switch {
		case op == 0x01: // nop
			continue
		case op == 0x20 || op == 0x21 || op == 0x22:
			index, err := r.U32()
			if err != nil || uint64(index) >= uint64(len(types)) || types[index] != mtI32 {
				return false
			}
			local, ok := p.local(index)
			if !ok {
				return false
			}
			if op == 0x20 {
				id = p.locals[local].value
			} else {
				if depth == 0 {
					return false
				}
				p.locals[local].value = stack[depth-1]
				p.locals[local].written = true
				if op == 0x21 {
					depth--
				}
				continue
			}
		case op == 0x41:
			value, err := r.I32()
			if err != nil {
				return false
			}
			id = p.add(integerMapNode{op: op, bits: uint64(uint32(value)), pc: pc})
		case op == 0x28: // i32.load
			if depth == 0 {
				return false
			}
			var imm wasm.InstructionImmediate
			if classifier.ClassifyInto(&r, op, &imm) != nil || imm.MemIndex != 0 || imm.MemOffset > uint64(^uint32(0)) {
				return false
			}
			depth--
			address := stack[depth]
			id = p.add(integerMapNode{op: op, a: address, bits: imm.MemOffset, pc: pc})
			if id == 0 || !p.effect(integerMapEffect{op: op, address: address, value: id, offset: imm.MemOffset, pc: pc}) {
				return false
			}
		case op == 0x36: // i32.store
			if depth < 2 {
				return false
			}
			var imm wasm.InstructionImmediate
			if classifier.ClassifyInto(&r, op, &imm) != nil || imm.MemIndex != 0 || imm.MemOffset > uint64(^uint32(0)) {
				return false
			}
			depth -= 2
			if !p.effect(integerMapEffect{op: op, address: stack[depth], value: stack[depth+1], offset: imm.MemOffset, pc: pc}) {
				return false
			}
			continue
		case op == 0x1a: // drop, including potentially trapping loads
			if depth == 0 {
				return false
			}
			depth--
			continue
		case op == 0x1b: // select
			if depth < 3 {
				return false
			}
			depth -= 3
			id = p.add(integerMapNode{op: op, a: stack[depth], b: stack[depth+1], c: stack[depth+2], pc: pc})
		case op == 0x45: // i32.eqz
			if depth == 0 {
				return false
			}
			depth--
			id = p.add(integerMapNode{op: op, a: stack[depth], pc: pc})
		case op >= 0x46 && op <= 0x4f || op >= 0x6a && op <= 0x6c || op >= 0x71 && op <= 0x76:
			if depth < 2 {
				return false
			}
			depth -= 2
			id = p.add(integerMapNode{op: op, a: stack[depth], b: stack[depth+1], pc: pc})
		case op == 0x0d: // a single final conditional backedge
			label, err := r.U32()
			if err != nil || label != 0 || depth != 1 {
				return false
			}
			end, err := r.Byte()
			if err != nil || end != 0x0b {
				return false
			}
			p.predicate = stack[0]
			p.endPC = start + r.Offset()
			return p.effectN != 0
		default:
			return false
		}
		if id == 0 || depth == len(stack) {
			return false
		}
		stack[depth] = id
		depth++
	}
	return false
}

// integerMapAffine describes a modular i32 expression in terms of the values
// at iteration entry. Widened memory immediates stay on effects separately.
type integerMapAffine struct {
	constant uint32
	coeff    [integerMapMaxLocals]uint32
	valid    bool
}

func (a integerMapAffine) constantOnly() bool {
	if !a.valid {
		return false
	}
	for _, c := range a.coeff {
		if c != 0 {
			return false
		}
	}
	return true
}

// affineFacts is one forward pass over the captured candidate. Children precede
// parents, so shared subexpressions never require repeated recursive walks.
func (p *integerMapPlan) affineFacts() (facts [integerMapMaxNodes + 1]integerMapAffine) {
	for i := uint8(1); i <= p.nodeN; i++ {
		n := p.nodes[i]
		switch n.op {
		case 0x20:
			if n.bits < uint64(p.localN) {
				facts[i].valid = true
				facts[i].coeff[n.bits] = 1
			}
		case 0x41:
			facts[i].valid = true
			facts[i].constant = uint32(n.bits)
		case 0x6a, 0x6b:
			a, b := facts[n.a], facts[n.b]
			if !a.valid || !b.valid {
				continue
			}
			if n.op == 0x6a {
				a.constant += b.constant
				for j, c := range b.coeff {
					a.coeff[j] += c
				}
			} else {
				a.constant -= b.constant
				for j, c := range b.coeff {
					a.coeff[j] -= c
				}
			}
			facts[i] = a
		case 0x6c, 0x74:
			a, b := facts[n.a], facts[n.b]
			if n.op == 0x74 {
				if !b.constantOnly() {
					continue
				}
				b.constant = uint32(1) << (b.constant & 31)
			} else if !b.constantOnly() {
				a, b = b, a
			}
			if !a.valid || !b.constantOnly() {
				continue
			}
			a.constant *= b.constant
			for j := range a.coeff {
				a.coeff[j] *= b.constant
			}
			facts[i] = a
		}
	}
	return facts
}

// addressStride derives a modular per-iteration change only when every entry
// local used by the address is unchanged or has an exact affine self-update.
// It does not prove non-wrapping addresses, bounds, or memory independence.
func (p *integerMapPlan) addressStride(facts *[integerMapMaxNodes + 1]integerMapAffine, id uint8) (uint32, bool) {
	a := facts[id]
	if !a.valid {
		return 0, false
	}
	var stride uint32
	for i, c := range a.coeff {
		if c == 0 {
			continue
		}
		if i >= int(p.localN) {
			return 0, false
		}
		l := p.locals[i]
		if !l.written {
			continue
		}
		update := facts[l.value]
		if !update.valid {
			return 0, false
		}
		for j, coefficient := range update.coeff {
			want := uint32(0)
			if i == j {
				want = 1
			}
			if coefficient != want {
				return 0, false
			}
		}
		stride += c * update.constant
	}
	return stride, true
}

// integerMapInduction identifies an exact self-update compared at the loop
// latch. This is a candidate only; runtime trip, wrap, and exit proofs are
// required before changing execution.
type integerMapInduction struct {
	local, limit, predicate uint8
	step                    uint32
	reversed                bool
}

func (p *integerMapPlan) induction(facts *[integerMapMaxNodes + 1]integerMapAffine) (integerMapInduction, bool) {
	pred := p.nodes[p.predicate]
	if pred.op < 0x46 || pred.op > 0x4f {
		return integerMapInduction{}, false
	}
	var out integerMapInduction
	found := false
	for i, l := range p.locals[:p.localN] {
		if !l.written {
			continue
		}
		update := facts[l.value]
		if !update.valid || update.constant == 0 {
			continue
		}
		valid := true
		for j, c := range update.coeff {
			want := uint32(0)
			if i == j {
				want = 1
			}
			if c != want {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		var limit uint8
		reversed := false
		if pred.a == l.value {
			limit = pred.b
		} else if pred.b == l.value {
			limit = pred.a
			reversed = true
		} else {
			continue
		}
		bound := facts[limit]
		if !bound.valid {
			continue
		}
		for j, c := range bound.coeff {
			if c != 0 && (j >= int(p.localN) || p.locals[j].written) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		if found {
			return integerMapInduction{}, false
		}
		out = integerMapInduction{local: uint8(i), limit: limit, predicate: pred.op, step: update.constant, reversed: reversed}
		found = true
	}
	return out, found
}

// requiredNodes marks original effects, the latch, and every changed local.
// Descending node order works because every captured child precedes its parent.
// An unused load remains a root: it can still trap.
func (p *integerMapPlan) requiredNodes() (used [integerMapMaxNodes + 1]bool) {
	used[p.predicate] = true
	for _, e := range p.effects[:p.effectN] {
		used[e.address] = true
		used[e.value] = true
	}
	for _, l := range p.locals[:p.localN] {
		if l.written {
			used[l.value] = true
		}
	}
	for i := int(p.nodeN); i > 0; i-- {
		if !used[i] {
			continue
		}
		n := p.nodes[i]
		switch {
		case n.op == 0x28 || n.op == 0x45:
			used[n.a] = true
		case n.op == 0x1b:
			used[n.a] = true
			used[n.b] = true
			used[n.c] = true
		case n.op >= 0x46 && n.op <= 0x4f || n.op >= 0x6a && n.op <= 0x6c || n.op >= 0x71 && n.op <= 0x76:
			used[n.a] = true
			used[n.b] = true
		}
	}
	used[0] = false
	return used
}

// independentInputs rejects an entry value of a written non-induction local
// when any required effect or published output still depends on that value.
// A source local that is overwritten before use does not create carried state.
func (p *integerMapPlan) independentInputs(ind integerMapInduction, used *[integerMapMaxNodes + 1]bool) bool {
	for i, l := range p.locals[:p.localN] {
		if l.written && i != int(ind.local) && used[l.entry] {
			return false
		}
	}
	return true
}

const integerMapMaxAliasPairs = 24

type integerMapStream struct {
	address     integerMapAffine
	offset      uint64
	stride      uint32
	read, write bool
}
type integerMapAliasPair struct{ a, b uint8 }
type integerMapMemoryPlan struct {
	streams        [integerMapMaxEffects]integerMapStream
	effectStream   [integerMapMaxEffects]uint8
	pairs          [integerMapMaxAliasPairs]integerMapAliasPair
	streamN, pairN uint8
}

// disjointWordResidues proves two word streams cannot overlap when their
// common power-of-two stride separates their lane addresses modulo the stride.
// Each stream still needs an independent bounds and no-wrap guard.
func disjointWordResidues(a, b integerMapStream) bool {
	stride := a.stride
	if stride < 8 || stride != b.stride || stride&(stride-1) != 0 || a.address.coeff != b.address.coeff {
		return false
	}
	delta := (b.address.constant - a.address.constant + uint32(b.offset) - uint32(a.offset)) & (stride - 1)
	return delta >= 4 && delta <= stride-4
}

// memoryPlan collects small affine memory streams and the pairs for which a
// future recipe must prove non-overlap. This is not a bounds, wrap, alias, or
// trap-order proof. In particular, an invariant load cannot alias a store.
func (p *integerMapPlan) memoryPlan(facts *[integerMapMaxNodes + 1]integerMapAffine, ind integerMapInduction, used *[integerMapMaxNodes + 1]bool) (m integerMapMemoryPlan, ok bool) {
	if !p.independentInputs(ind, used) {
		return m, false
	}
	stores := 0
	for i, e := range p.effects[:p.effectN] {
		a := facts[e.address]
		stride, valid := p.addressStride(facts, e.address)
		// The initial recipes operate on forward word streams and invariant loads.
		// Later target recipes may admit other strides with their own proofs.
		if !valid || stride >= 1<<31 || stride%4 != 0 || (e.op == 0x36 && stride == 0) {
			return m, false
		}
		if e.op == 0x36 {
			stores++
		}
		stream := -1
		for j, s := range m.streams[:m.streamN] {
			if s.address == a && s.offset == e.offset && s.stride == stride {
				stream = j
				break
			}
		}
		if stream < 0 {
			stream = int(m.streamN)
			m.streamN++
			m.streams[stream] = integerMapStream{address: a, offset: e.offset, stride: stride}
		}
		m.effectStream[i] = uint8(stream)
		if e.op == 0x28 {
			m.streams[stream].read = true
		} else if e.op == 0x36 {
			m.streams[stream].write = true
		} else {
			return m, false
		}
	}
	if stores == 0 {
		return m, false
	}
	for i, a := range m.streams[:m.streamN] {
		if !a.write {
			continue
		}
		for j, b := range m.streams[:m.streamN] {
			if i == j || b.write && j < i || disjointWordResidues(a, b) {
				continue
			}
			if int(m.pairN) == len(m.pairs) {
				return m, false
			}
			m.pairs[m.pairN] = integerMapAliasPair{a: uint8(i), b: uint8(j)}
			m.pairN++
		}
	}
	return m, true
}
