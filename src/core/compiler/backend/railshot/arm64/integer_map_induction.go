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
		increment, limitNode = c.a, c.b
		if p.nodes[increment].op != 0x6a {
			increment, limitNode = limitNode, increment
		}
	}
	inc := p.nodes[increment]
	if inc.op != 0x6a {
		return false
	}
	local, stride := p.nodes[inc.a], p.nodes[inc.b]
	if local.op != 0x20 {
		local, stride = stride, local
	}
	if local.op != 0x20 || stride.op != 0x41 || stride.bits == 0 || stride.bits > 32 {
		return false
	}
	slot := p.localSlot(local.a)
	if slot < 0 || p.locals[slot].Type != mtI32 || p.locals[slot].Value != increment {
		return false
	}
	p.counter = local.a
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
			s := p.localSlot(limit.a)
			if s < 0 || p.locals[s].Type != mtI32 || p.locals[s].Written {
				return false
			}
			p.limitLocal = limit.a
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
			return n.op == 0x6a && walk(n.a) && walk(n.b)
		}
		if !walk(cell.Value) || reads != 1 || delta != p.step {
			return false
		}
		p.byteIndices |= 1 << i
	}
	return true
}

// An address has at most one advancing byte index, one unchanged base and a
// modular constant. Memory immediates remain separate widened additions.
func (p *floatMapPlan) integerAffine(root int) (floatMapAddress, bool) {
	base, index := -1, -1
	var bias uint32
	var walk func(int) bool
	walk = func(x int) bool {
		if !p.spend() {
			return false
		}
		n := p.nodes[x]
		switch n.op {
		case 0x41:
			bias += uint32(n.bits)
			return true
		case 0x20:
			slot := p.localSlot(n.a)
			if slot < 0 || p.locals[slot].Type != mtI32 {
				return false
			}
			advancing := p.byteIndices&(1<<slot) != 0 || n.a == p.counter && p.spanScale == 1
			if advancing {
				if index >= 0 {
					return false
				}
				index = n.a
				return true
			}
			if p.locals[slot].Written || base >= 0 {
				return false
			}
			base = n.a
			return true
		case 0x6a:
			return walk(n.a) && walk(n.b)
		}
		return false
	}
	if !walk(root) || bias > 1<<20 {
		return floatMapAddress{}, false
	}
	return floatMapAddress{base: base, index: index + 1, bias: bias, invariant: index < 0}, true
}
