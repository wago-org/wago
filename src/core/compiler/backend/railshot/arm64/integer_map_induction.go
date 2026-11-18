// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

// Summarize the entry-to-exit updates of one source packet. A trip counter can
// count elements while independent address locals advance in bytes. Only small
// exact power-of-two ratios are admitted; no local histories escape the plan.
func (p *floatMapPlan) finishIntegerInduction(condition, stores int, initialRead []bool) bool {
	increment := condition
	limitNode := -1
	p.limitLocal = -1
	if c := p.nodes[condition]; c.op == 0x47 {
		increment, limitNode = c.left(), c.right()
		if p.nodes[increment].op != 0x6a {
			increment, limitNode = limitNode, increment
		}
	}
	inc := p.nodes[increment]
	if inc.op != 0x6a {
		return false
	}
	local, stride := p.nodes[inc.left()], p.nodes[inc.right()]
	if local.op != 0x20 {
		local, stride = stride, local
	}
	if local.op != 0x20 || stride.op != 0x41 || stride.bits == 0 || stride.bits > 32 {
		return false
	}
	slot := p.localSlot(local.left())
	if slot < 0 || p.locals[slot].Type != mtI32 || p.locals[slot].Value != increment {
		return false
	}
	p.counter = local.left()
	size := uint32(4)
	if p.wide {
		size = 8
	}
	p.step = uint32(stores) * size
	p.counterStep = uint32(stride.bits)
	if p.step%p.counterStep != 0 {
		return false
	}
	p.spanScale = p.step / p.counterStep
	if p.spanScale > 8 || p.spanScale&(p.spanScale-1) != 0 {
		return false
	}
	p.packet = floatMapPacketWidth(p.step)
	if p.packet == 0 {
		return false
	}
	if limitNode >= 0 {
		limit := p.nodes[limitNode]
		switch limit.op {
		case 0x41:
			p.limit = uint32(limit.bits)
		case 0x20:
			s := p.localSlot(limit.left())
			if s < 0 || p.locals[s].Type != mtI32 || p.locals[s].Written {
				return false
			}
			p.limitLocal = limit.left()
		default:
			return false
		}
	}
	for i, cell := range p.locals[:p.localN] {
		if !p.spend() {
			return false
		}
		if !cell.Written || !initialRead[i] || int(cell.Index) == p.counter {
			continue
		}
		if cell.Type != mtI32 {
			return false
		}
		var delta uint32
		reads := 0
		var walk func(int) bool
		walk = func(x int) bool {
			if !p.spend() {
				return false
			}
			if x == cell.Initial {
				reads++
				return reads <= 1
			}
			n := p.nodes[x]
			if n.op == 0x41 {
				delta += uint32(n.bits)
				return true
			}
			return n.op == 0x6a && walk(n.left()) && walk(n.right())
		}
		if !walk(cell.Value) || reads != 1 || delta == 0 || delta%p.step != 0 || delta/p.step > 256 {
			return false
		}
		p.byteSteps[i] = delta
		p.byteIndices |= 1 << i
	}
	return true
}

// An address has at most one advancing byte index, one unchanged base and a
// modular constant. Memory immediates remain separate widened additions.
func (p *floatMapPlan) integerAffine(root int) (floatMapAddress, bool) {
	base, index := -1, -1
	var bias, stride uint32
	var walk func(int, uint32) bool
	walk = func(x int, scale uint32) bool {
		if !p.spend() {
			return false
		}
		n := p.nodes[x]
		switch n.op {
		case 0x41:
			bias += scale * uint32(n.bits)
			return true
		case 0x20:
			slot := p.localSlot(n.left())
			if slot < 0 || p.locals[slot].Type != mtI32 {
				return false
			}
			if n.left() == p.counter {
				if index >= 0 || scale == 0 || scale%p.spanScale != 0 || scale/p.spanScale > 256 {
					return false
				}
				index, stride = n.left(), scale/p.spanScale
				return true
			}
			if p.byteIndices&(1<<slot) != 0 {
				if index >= 0 || scale != 1 {
					return false
				}
				index, stride = n.left(), p.byteSteps[slot]/p.step
				return true
			}
			if p.locals[slot].Written || base >= 0 || scale != 1 {
				return false
			}
			base = n.left()
			return true
		case 0x6a:
			return walk(n.left(), scale) && walk(n.right(), scale)
		case 0x6c:
			if !p.integer {
				return false
			}
			left, right := n.left(), n.right()
			if p.nodes[left].op == 0x41 {
				left, right = right, left
			}
			if constant := p.nodes[right]; constant.op == 0x41 {
				return walk(left, scale*uint32(constant.bits))
			}
		case 0x74:
			if p.integer && n.immediateShift() {
				return walk(n.left(), scale<<uint32(n.bits&31))
			}
		}
		return false
	}
	if !walk(root, 1) || bias > 1<<20 {
		return floatMapAddress{}, false
	}
	if stride == 1 {
		stride = 0
	}
	return floatMapAddress{base: base, index: index + 1, bias: bias, invariant: index < 0, stride: stride}, true
}

// mapStoreBase identifies a candidate immutable base, leaving address
// legality and disjointness to the existing normalized proof and guards.
func (p *floatMapPlan) mapStoreBase(root, depth int) int {
	if depth > 8 || !p.spend() {
		return -1
	}
	n := p.nodes[root]
	switch n.op {
	case 0x20:
		if !p.localWritten(n.left()) {
			return n.left()
		}
	case 0x6a:
		if left := p.mapStoreBase(n.left(), depth+1); left >= 0 {
			return left
		}
		return p.mapStoreBase(n.right(), depth+1)
	}
	return -1
}

// One logical word per iteration gives each native lane a different source
// counter. Source-unrolled adjacent stores need a separate per-store proof.
func (p *floatMapPlan) mapCounterValue(node floatMapNode) bool {
	return p.integer && p.step == 4 && node.op == 0x20 && node.left() == p.counter
}

type integerMapExitPlan struct {
	written uint32 // function-local coordinates
	extra   uint32 // candidate-local coordinates
	counter uint32
	bias    [32]uint32
}

func (f *fn) planIntegerMapExit(p *floatMapPlan) integerMapExitPlan {
	if !integerMapAffineExitEnabled || !p.integer || f.vectorLoopExit != nil || f.usesCalls || f.nLocals > 32 ||
		f.depth() != 0 || len(f.intervalLast) != 0 || f.preserveCallerPins {
		return integerMapExitPlan{}
	}
	var exit integerMapExitPlan
	for i, cell := range p.locals[:p.localN] {
		if !cell.Written {
			continue
		}
		exit.written |= uint32(1) << cell.Index
		bit := uint32(1) << i
		if int(cell.Index) == p.counter || p.byteIndices&bit != 0 {
			continue
		}
		bias, usesCounter, ok := p.integerMapExitValue(cell.Value, 0)
		if !ok || cell.Type != mtI32 {
			return integerMapExitPlan{}
		}
		exit.extra |= bit
		if usesCounter {
			exit.counter |= bit
		}
		exit.bias[i] = bias
	}
	return exit
}

func (p *floatMapPlan) integerMapExitValue(x, depth int) (bias uint32, usesCounter, ok bool) {
	if depth > 8 || !p.spend() {
		return 0, false, false
	}
	v := p.nodes[x]
	switch v.op {
	case 0x41:
		return uint32(v.bits), false, true
	case 0x20:
		return 0, true, v.left() == p.counter
	case 0x6a:
		a, left, ok := p.integerMapExitValue(v.left(), depth+1)
		if !ok {
			return 0, false, false
		}
		b, right, ok := p.integerMapExitValue(v.right(), depth+1)
		return a + b, left || right, ok && !(left && right)
	}
	return 0, false, false
}
