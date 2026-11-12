// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

// integerMapSLP is a short-lived schedule for two adjacent output expressions.
// A vector contains two source iterations, with the two outputs interleaved.
// Its registers are indices into the already leased vector scratch, not native
// allocator identities. Unsupported shapes leave the ordinary packet unchanged.
type integerMapSLPNode struct {
	left, right uint8
	a, b, c     uint8
	reg         uint8
	op          byte
	stream      uint8
	swap        bool
}
type integerMapSLP struct {
	nodes     [integerMapMaxNodes + 1]integerMapSLPNode
	count     uint8
	root      uint8
	localNode [integerMapMaxLocals]uint8
	localLane [integerMapMaxLocals]byte
	relations [integerMapMaxEffects]integerMapAliasPair // high, low: high must equal low+4
	relationN uint8
}

// buildIntegerMapSLP matches expressions in lockstep, without changing emitter
// or allocator state. Source-ordered effects and packet guards remain those of
// the underlying adjacent-store plan. Every required non-affine local must be
// represented by the selected pair and remains live until publication.
func buildIntegerMapSLP(p *integerMapPlan, g *integerMapGuards, r *integerMapRecipe, ind integerMapInduction) (plan integerMapSLP, ok bool) {
	if !g.packStores || g.lanes != 4 || r.used[p.locals[ind.local].entry] {
		return plan, false
	}
	work := 0
	var match func(uint8, uint8, int) (uint8, bool)
	match = func(left, right uint8, depth int) (uint8, bool) {
		work++
		if left == 0 || right == 0 || depth > 32 || work > 1024 {
			return 0, false
		}
		for i := uint8(1); i <= plan.count; i++ {
			work++
			if work > 1024 {
				return 0, false
			}
			if plan.nodes[i].left == left && plan.nodes[i].right == right {
				return i, true
			}
		}
		x, y := p.nodes[left], p.nodes[right]
		if x.op != y.op {
			return 0, false
		}
		n := integerMapSLPNode{left: left, right: right, op: x.op}
		switch {
		case x.op == 0x20:
			if x.bits != y.bits || x.bits >= uint64(p.localN) || uint8(x.bits) == ind.local || p.locals[x.bits].written {
				return 0, false
			}
		case x.op == 0x41:
			if x.bits != y.bits {
				return 0, false
			}
		case x.op == 0x28:
			a, b := int(r.loadStream[left]), int(r.loadStream[right])
			if a >= int(g.memory.streamN) || b >= int(g.memory.streamN) {
				return 0, false
			}
			as, bs := g.memory.streams[a], g.memory.streams[b]
			if as.stride == 0 && bs.stride == 0 && a == b {
				n.stream = uint8(a)
				break
			}
			baseA, deltaA := shareStream(&g.memory, a)
			baseB, deltaB := shareStream(&g.memory, b)
			if as.stride != 8 || bs.stride != 8 {
				return 0, false
			}
			if baseA == baseB && (deltaA == 0 && deltaB == 4 || deltaA == 4 && deltaB == 0) {
				n.stream, n.swap = uint8(baseA), deltaA == 4
				break
			}
			if as.offset != bs.offset || as.address.coeff != bs.address.coeff {
				return 0, false
			}
			high, low := a, b
			if bs.address.constant-as.address.constant == 4 {
				high, low = b, a
			} else if as.address.constant-bs.address.constant != 4 {
				return 0, false
			}
			found := false
			for _, rel := range plan.relations[:plan.relationN] {
				if int(rel.a) == high && int(rel.b) == low {
					found = true
				}
			}
			if !found {
				if int(plan.relationN) == len(plan.relations) {
					return 0, false
				}
				plan.relations[plan.relationN] = integerMapAliasPair{a: uint8(high), b: uint8(low)}
				plan.relationN++
			}
			n.stream, n.swap = uint8(low), high == a
		case x.op == 0x1b && r.minSelect[left] && r.minSelect[right] || x.op >= 0x6a && x.op <= 0x6c || x.op >= 0x71 && x.op <= 0x73:
			checkpoint, relationCheckpoint := plan.count, plan.relationN
			var valid bool
			n.a, valid = match(x.a, y.a, depth+1)
			if valid {
				n.b, valid = match(x.b, y.b, depth+1)
			}
			if !valid && x.op != 0x6b { // subtraction is not commutative
				plan.count = checkpoint
				plan.relationN = relationCheckpoint
				n.a, valid = match(x.a, y.b, depth+1)
				if valid {
					n.b, valid = match(x.b, y.a, depth+1)
				}
			}
			if !valid {
				return 0, false
			}
			if x.op == 0x1b {
				n.op = 0xff // exact signed minimum
			}
		default:
			return 0, false
		}
		if int(plan.count) == integerMapMaxNodes {
			return 0, false
		}
		plan.count++
		plan.nodes[plan.count] = n
		return plan.count, true
	}
	var found bool
	plan.root, found = match(p.effects[g.packFirst].value, p.effects[g.packSecond].value, 0)
	if !found {
		return integerMapSLP{}, false
	}
	var uses [integerMapMaxNodes + 1]uint16
	for i := uint8(1); i <= plan.count; i++ {
		n := plan.nodes[i]
		if n.a != 0 {
			uses[n.a]++
		}
		if n.b != 0 {
			uses[n.b]++
		}
		if n.c != 0 {
			uses[n.c]++
		}
	}
	uses[plan.root]++ // output store
	for i, l := range p.locals[:p.localN] {
		if !l.written || i == int(ind.local) || r.exitAffine[i] {
			continue
		}
		for j := plan.count; j != 0; j-- {
			n := plan.nodes[j]
			if n.right == l.value {
				plan.localNode[i], plan.localLane[i] = j, 3
				break
			}
			if n.left == l.value {
				plan.localNode[i], plan.localLane[i] = j, 2
				break
			}
		}
		if plan.localNode[i] == 0 {
			return integerMapSLP{}, false
		}
		uses[plan.localNode[i]]++
	}
	var occupied [16]bool
	for i := uint8(1); i <= plan.count; i++ {
		reg := uint8(0)
		for reg < r.registers-2 && occupied[reg] {
			reg++
		}
		if reg >= r.registers-2 {
			return integerMapSLP{}, false
		}
		occupied[reg] = true
		plan.nodes[i].reg = reg
		n := plan.nodes[i]
		for _, child := range [...]uint8{n.a, n.b, n.c} {
			if child != 0 {
				uses[child]--
				if uses[child] == 0 {
					occupied[plan.nodes[child].reg] = false
				}
			}
		}
	}
	return plan, true
}

// emitSLP emits two iterations at a time. The caller has already checked the
// enclosing four-iteration packet's bounds and dependencies. Only the paired
// store is issued here; the original scalar path remains the fallback.
func (e *integerMapBodyEmitter) emitSLP() {
	p, plan := e.plan, e.slp
	for i := uint8(1); i <= plan.count; i++ {
		n := plan.nodes[i]
		out := e.fp[n.reg]
		switch n.op {
		case 0x20:
			e.a.Load32(e.gp[2], RSP, e.guard.slot(int(p.nodes[n.left].bits)))
			e.a.MovGprToXmm(out, e.gp[2], false)
			e.a.Pshufd(out, out, 0)
		case 0x41:
			e.broadcast(out, uint32(p.nodes[n.left].bits))
		case 0x28:
			disp := e.streamStart(int(n.stream), e.gp[1])
			if e.guard.plan.memory.streams[n.stream].stride == 0 {
				e.a.LoadIdx(e.gp[2], e.memoryBase, e.gp[1], disp, 4, false, false)
				e.a.MovGprToXmm(out, e.gp[2], false)
				e.a.Pshufd(out, out, 0)
			} else {
				e.a.MovdquLoadIdx(out, e.memoryBase, e.gp[1], disp)
				if n.swap {
					e.a.Pshufd(out, out, 0xb1)
				}
			}
		case 0xff:
			e.copy(out, e.fp[plan.nodes[n.a].reg])
			e.a.SseMapRR(0x66, 0x38, 0x39, out, e.fp[plan.nodes[n.b].reg])
		default:
			e.copy(out, e.fp[plan.nodes[n.a].reg])
			right := e.fp[plan.nodes[n.b].reg]
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
				panic("unsupported integer map SLP operation")
			}
		}
	}
	stream := int(e.guard.plan.memory.effectStream[e.guard.plan.packFirst])
	disp := e.streamStart(stream, e.gp[1])
	e.a.MovdquStoreIdx(e.memoryBase, e.gp[1], e.fp[plan.nodes[plan.root].reg], disp)
}

// emitSLPWide uses the same legal plan and register assignment for four
// iterations, with one adjacent output pair in each pair of native lanes.
func (e *integerMapBodyEmitter) emitSLPWide() {
	p, plan := e.plan, e.slp
	for i := uint8(1); i <= plan.count; i++ {
		n := plan.nodes[i]
		out := e.fp[n.reg]
		switch n.op {
		case 0x20:
			e.a.YBroadcastDLoadDisp(out, RSP, e.guard.slot(int(p.nodes[n.left].bits)))
		case 0x41:
			e.a.MovImm32(e.gp[2], int32(p.nodes[n.left].bits))
			e.a.Store32(RSP, e.outputBase, e.gp[2])
			e.a.YBroadcastDLoadDisp(out, RSP, e.outputBase)
		case 0x28:
			addr, disp := e.streamReadAddress(int(n.stream), e.gp[1])
			if e.guard.plan.memory.streams[n.stream].stride == 0 {
				e.a.YBroadcastDLoadIdx(out, e.memoryBase, addr, disp)
			} else {
				e.a.YMovdquLoadIdx(out, e.memoryBase, addr, disp)
				if n.swap {
					e.a.YPshufd(out, out, 0xb1)
				}
			}
		default:
			left, right := e.fp[plan.nodes[n.a].reg], e.fp[plan.nodes[n.b].reg]
			switch n.op {
			case 0xff:
				e.a.YPminsd(out, left, right)
			case 0x6a:
				e.a.YPaddd(out, left, right)
			case 0x6b:
				e.a.YPsubd(out, left, right)
			case 0x6c:
				e.a.YPmulld(out, left, right)
			case 0x71:
				e.a.YPand(out, left, right)
			case 0x72:
				e.a.YPor(out, left, right)
			case 0x73:
				e.a.YPxor(out, left, right)
			default:
				panic("unsupported wide integer map SLP operation")
			}
		}
	}
	stream := int(e.guard.plan.memory.effectStream[e.guard.plan.packFirst])
	addr, disp := e.streamReadAddress(stream, e.gp[1])
	e.a.YMovdquStoreIdx(e.memoryBase, addr, e.fp[plan.nodes[plan.root].reg], disp)
}
