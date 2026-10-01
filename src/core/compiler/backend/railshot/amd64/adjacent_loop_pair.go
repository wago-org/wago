//go:build amd64

package amd64

// A bounded exact matcher joins two adjacent scalar outputs. It keeps operand
// order and performs no FP reduction reassociation. Both original outputs are
// represented by the low/high lanes of the left tree. Guards must subsequently
// prove the full ranges and absence of cross-stream aliasing.
func (p *regionLoopPlan) packAdjacentOutputs() bool {
	if p.storeN != 2 {
		return false
	}
	for i, l := range p.locals[:p.localN] {
		if l.typ == mtF64 && l.written && p.inputMask&(1<<i) != 0 {
			return false
		}
	}
	first, second := p.stores[0], p.stores[1]
	if p.stride(first.address) != 16 || !p.adjacentAddresses(first.address, first.offset, second.address, second.offset) {
		return false
	}
	var representative, lane [regionLoopMaxOps + 1]uint8
	var match func(uint8, uint8) bool
	match = func(left, right uint8) bool {
		if left == 0 || right == 0 {
			return false
		}
		if left == right {
			n := p.nodes[left]
			if n.op >= 0xa0 && n.op <= 0xa3 || (n.op == 0x2b && p.stride(n.left) != 0) {
				return false
			}
		}
		if representative[right] != 0 {
			return representative[right] == left && representative[left] == left
		}
		if representative[left] != 0 && representative[left] != left {
			return false
		}
		a, b := p.nodes[left], p.nodes[right]
		if a.op != b.op {
			return false
		}
		switch a.op {
		case 0x20:
			if left != right {
				return false
			}
		case 0x44:
			if a.bits != b.bits {
				return false
			}
		case 0x2b:
			stride := p.stride(a.left)
			if stride == 0 {
				x, y := p.nodes[a.left], p.nodes[b.left]
				if x.coeff != y.coeff || x.bits != y.bits || a.bits != b.bits {
					return false
				}
			} else if stride != 16 || !p.adjacentAddresses(a.left, a.bits, b.left, b.bits) {
				return false
			}
		case 0xa0, 0xa1, 0xa2, 0xa3:
			if left == right || !match(a.left, b.left) || !match(a.right, b.right) {
				return false
			}
		default:
			return false
		}
		// A node previously matched as a left lane cannot become a distinct right
		// lane. This excludes tangled producer ownership and serial dependencies.
		if left != right && lane[right]&1 != 0 {
			return false
		}
		representative[left], representative[right] = left, left
		lane[left] |= 1
		lane[right] |= 2
		return true
	}
	if !match(first.value, second.value) {
		return false
	}
	// Every retained FP event must participate in the pair. Reject dead or
	// unrelated loads rather than erase their possible traps or local effects.
	for _, event := range p.events[:p.eventN] {
		if event&0x80 == 0 && representative[event] == 0 {
			return false
		}
	}
	for i, l := range p.locals[:p.localN] {
		if l.typ == mtF64 && l.written {
			if representative[l.value] == 0 {
				return false
			}
			if lane[l.value] == 2 {
				p.exitHigh |= 1 << i
			}
		}
	}
	at := 0
	for _, event := range p.events[:p.eventN] {
		if event&0x80 != 0 {
			if event == 0x80 {
				p.events[at] = 0x80
				at++
			}
		} else if representative[event] == event {
			p.events[at] = event
			at++
		}
	}
	p.eventN = uint8(at)
	p.storeN = 1
	p.loadN = 0
	for _, event := range p.events[:p.eventN] {
		if event&0x80 != 0 {
			continue
		}
		n := p.nodes[event]
		if n.op == 0x2b {
			width := uint8(8)
			stride := p.stride(n.left)
			if stride != 0 {
				width = 16
			}
			p.loads[p.loadN] = regionLoopLoad{node: event, bytes: width, stride: stride}
			p.loadN++
		}
	}
	for i := range p.locals[:p.localN] {
		l := &p.locals[i]
		if l.typ == mtF64 && l.written {
			l.value = representative[l.value]
		}
	}
	p.adjacent = true
	return true
}

// Both forms preserve memory32's wrapped address before its widened immediate.
// A successful width-16 range proof for the first stream prevents the small
// affine displacement from wrapping before the second lane.
func (p *regionLoopPlan) adjacentAddresses(left uint8, leftOffset uint64, right uint8, rightOffset uint64) bool {
	a, b := p.nodes[left], p.nodes[right]
	if a.op != 0 || b.op != 0 || a.coeff != b.coeff || rightOffset < leftOffset {
		return false
	}
	delta := uint32(b.bits) - uint32(a.bits)
	return delta <= 8 && uint64(delta)+rightOffset-leftOffset == 8
}
func (p *regionLoopPlan) iterationsPerVector() uint32 {
	if p.wide && !p.adjacent && !p.scalar {
		return 4
	}
	if (p.adjacent && !p.wide) || p.scalar {
		return 1
	}
	return 2
}
