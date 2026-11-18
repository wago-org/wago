// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"os"
)

var streamingPureSelectEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_PURE_SELECT") == "1"

// Only the bounded pending ALU spine may be skipped. All memory accesses,
// divisions and other effects have already executed in source order. Shared
// deferred intermediates stay on the eager path: their other readers must not
// inherit a result computed on just one native predecessor.
type streamPureSelectPlan struct {
	nodes         [pureSelectMaxOps]streamID
	leaf          streamID
	n, cost, muls int
}

func (s *streamState) pureSelectLeaf(id streamID, wide bool) bool {
	if id == 0 {
		return false
	}
	n := s.node(id)
	if n.refs == 0 || n.wide != wide {
		return false
	}
	switch n.kind {
	case shared.ScalarConstant, shared.ScalarRegister, shared.ScalarFrame, streamBacking:
		return true
	case streamBorrow:
		// Provisional only: capture this mutable frame/home alias before emission.
		return n.slot >= 0 && int(n.slot) < len(s.locals)
	}
	return false
}

func (s *streamState) provePureSelect(id streamID, wide bool) (p streamPureSelectPlan, ok bool) {
	for id != 0 && s.node(id).kind == streamDeferred {
		n := s.node(id)
		if p.n == len(p.nodes) || n.refs != 1 || n.wide != wide || n.operandWide != wide || !isBinALU(wOp(n.op)) || !s.pureSelectLeaf(n.right, wide) {
			return p, false
		}
		p.nodes[p.n] = id
		p.n++
		p.cost++
		if n.op == shared.IntMul {
			p.cost += 2
			p.muls++
		}
		id = n.left
	}
	p.leaf = id
	return p, s.pureSelectLeaf(id, wide) && p.muls != 0 && p.cost >= 4
}

// tryPureSelect consumes its three detached stack roots only on success. A
// failed proof preserves them for the ordinary eager selector, though condition
// or leaf capture may already have performed harmless eager materialization.
func (s *streamState) tryPureSelect(cond, right, left streamID) bool {
	if !streamingPureSelectEnabled || !pureSelectEnabled {
		return false
	}
	wide := s.node(left).wide
	if s.node(right).wide != wide {
		return false
	}
	costly, cheap, skipCC := left, right, condE
	p, ok := s.provePureSelect(costly, wide)
	if !ok || !s.pureSelectLeaf(cheap, wide) {
		costly, cheap, skipCC = right, left, condNE
		p, ok = s.provePureSelect(costly, wide)
		if !ok || !s.pureSelectLeaf(cheap, wide) {
			return false
		}
	}
	// Reserve room for condition plus two scratch registers. This is a physical
	// bank check, not permission to spill after the conditional branch.
	available := 0
	for _, r := range s.regs {
		if s.reserved&(uint64(1)<<r) == 0 {
			available++
		}
	}
	if available < 3 {
		return false
	}
	cr := s.materialize(cond, 0)
	saved := s.reserved
	s.reserved |= uint64(1) << cr
	defer func() { s.reserved = saved }()
	// Condition evaluation can resolve shared leaves or evict backing cells.
	// Stable IDs allow a complete reproof without retaining stale locations.
	p, ok = s.provePureSelect(costly, wide)
	if !ok || !s.pureSelectLeaf(cheap, wide) {
		return false
	}
	capture := func(id streamID) {
		if s.node(id).kind == streamBorrow {
			s.materialize(id, 0)
		}
	}
	capture(cheap)
	capture(p.leaf)
	for i := 0; i < p.n; i++ {
		capture(s.node(p.nodes[i]).right)
	}
	// Allocate/evacuate both scratch registers BEFORE resolving final operands.
	// No allocator operation is permitted from TEST through the branch join.
	result := s.alloc(0)
	s.reserved |= uint64(1) << result
	scratch := s.alloc(0)
	s.reserved |= uint64(1) << scratch
	var operands [pureSelectMaxOps]shared.ScalarOperand
	for i := 0; i < p.n; i++ {
		operands[i] = s.operand(s.node(p.nodes[i]).right)
	}
	cheapOp, baseOp := s.operand(cheap), s.operand(p.leaf)
	valid := func(o shared.ScalarOperand) bool {
		switch o.Kind {
		case shared.ScalarConstant, shared.ScalarFrame:
			return true
		case shared.ScalarRegister:
			return o.Reg != result && o.Reg != scratch
		}
		return false
	}
	if !valid(cheapOp) || !valid(baseOp) {
		return false
	}
	for i := 0; i < p.n; i++ {
		if !valid(operands[i]) {
			return false
		}
	}
	f, a := s.target, s.target.a
	s.forgetRegisterBacking(result)
	s.forgetRegisterBacking(scratch)
	// The read helper emits only MOV/load instructions; frame operands are owned
	// snapshots, never Wasm linear-memory accesses or mutable local identities.
	read := func(dst uint8, o shared.ScalarOperand) {
		switch o.Kind {
		case shared.ScalarConstant:
			f.Constant(dst, o.Constant, wide)
		case shared.ScalarRegister:
			f.Move(dst, o.Reg, wide)
		case shared.ScalarFrame:
			f.Load(dst, o.Offset, wide)
		}
	}
	read(result, cheapOp)
	// Result is distinct from every captured input register, so the i32
	// MOV/load/constant above always writes 32 bits on the skipped predecessor.
	a.TestSelf(Reg(cr), false)
	skip := a.JccPlaceholder(skipCC)
	read(result, baseOp)
	for i := p.n - 1; i >= 0; i-- {
		op := s.node(p.nodes[i]).op
		r := operands[i]
		if r.Kind == shared.ScalarConstant && (!wide || r.Constant == int64(int32(r.Constant))) {
			if op == shared.IntMul {
				a.ImulRI(Reg(result), int32(r.Constant), wide)
			} else {
				a.AluRI(aluTable[wOp(op)].digit, Reg(result), int32(r.Constant), wide)
			}
		} else {
			rr := r.Reg
			if r.Kind != shared.ScalarRegister {
				read(scratch, r)
				rr = scratch
			}
			if op == shared.IntMul {
				a.IMul(Reg(result), Reg(rr), wide)
			} else {
				a.AluRR(aluTable[wOp(op)].rr, Reg(result), Reg(rr), wide)
			}
		}
	}
	a.PatchRel32(skip, a.Len())
	s.integerResult(result, wide, cond, right, left)
	f.stats.peep("stream-pure-select-branch")
	return true
}
