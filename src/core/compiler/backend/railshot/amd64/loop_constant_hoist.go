//go:build amd64

package amd64

// hoistConstants moves at most two exact f64 literals into the guarded loop
// preheader. Literals cannot trap or observe memory. Every arithmetic, load and
// store retains its source order; equal literals share only identical raw bits.
// Permanent homes participate in the existing peak-register calculation.
// The trial plan is bounded worker scratch, used only by admitted regions.
func (p *regionLoopPlan) hoistConstants(enabled, arithmeticPrefix, memoryForms bool) (uint8, [regionLoopMaxOps + 1]bool) {
	var none [regionLoopMaxOps + 1]bool
	if !enabled {
		return 0, none
	}
	trial := *p
	prefix, keep := trial.prepareConstantPrefix(arithmeticPrefix)
	if prefix == 0 {
		return 0, none
	}
	folded, _ := trial.memoryForms(memoryForms)
	if trial.scratchNeedPermanent(folded, keep) > 10 {
		return 0, none
	}
	*p = trial
	return prefix, keep
}

func (p *regionLoopPlan) prepareConstantPrefix(arithmeticPrefix bool) (uint8, [regionLoopMaxOps + 1]bool) {
	var keep [regionLoopMaxOps + 1]bool
	var constants [2]uint8
	var canonical [regionLoopMaxOps + 1]uint8
	count := uint8(0)
	for _, event := range p.events[:p.eventN] {
		if event&0x80 != 0 || p.nodes[event].op != 0x44 {
			continue
		}
		id := uint8(0)
		for _, old := range constants[:count] {
			if p.nodes[old].bits == p.nodes[event].bits {
				id = old
				break
			}
		}
		if id == 0 {
			if count == uint8(len(constants)) {
				return 0, keep
			}
			id = event
			constants[count] = id
			count++
		}
		canonical[event] = id
	}
	if count == 0 {
		return 0, keep
	}
	remap := func(id uint8) uint8 {
		if canonical[id] != 0 {
			return canonical[id]
		}
		return id
	}
	for i := uint8(1); i <= p.nodeN; i++ {
		n := &p.nodes[i]
		if n.op >= 0xa0 && n.op <= 0xa3 {
			n.left, n.right = remap(n.left), remap(n.right)
		}
	}
	for i := uint8(0); i < p.storeN; i++ {
		p.stores[i].value = remap(p.stores[i].value)
	}
	for i := uint8(0); i < p.localN; i++ {
		if p.locals[i].typ == mtF64 {
			p.locals[i].value = remap(p.locals[i].value)
		}
	}
	var events [regionLoopMaxOps]uint8
	at := int(count)
	copy(events[:], constants[:count])
	for _, event := range p.events[:p.eventN] {
		if event&0x80 == 0 && p.nodes[event].op == 0x44 {
			continue
		}
		events[at] = event
		at++
	}
	p.events, p.eventN = events, uint8(at)
	// Prefer the stronger existing invariant arithmetic prefix when admitted.
	if prefix, permanent := p.invariantPrefix(arithmeticPrefix); prefix != 0 {
		return prefix, permanent
	}
	uses := p.fpUses()
	for _, id := range constants[:count] {
		if uses[id] == 0 {
			return 0, [regionLoopMaxOps + 1]bool{}
		}
		keep[id] = true
	}
	if count >= p.eventN {
		return 0, [regionLoopMaxOps + 1]bool{}
	}
	return count, keep
}
