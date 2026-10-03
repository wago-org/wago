//go:build amd64

package amd64

// invariantPrefix admits only a source-ordered prefix of pure FP events.
// Moving accesses and stores stop the prefix. Existing success-only range and
// alias guards prove every broadcast load safe and disjoint from all writes;
// failed guards still execute the original scalar loop. At most two values
// may survive the prefix, and their registers remain private across backedges.
func (p *regionLoopPlan) invariantPrefix(enabled bool) (uint8, [regionLoopMaxOps + 1]bool) {
	var permanent [regionLoopMaxOps + 1]bool
	if !enabled || !p.adjacent {
		return 0, permanent
	}
	var invariant [regionLoopMaxOps + 1]bool
	for _, l := range p.locals[:p.localN] {
		if l.typ == mtF64 && !l.written {
			invariant[l.initial] = true
		}
	}
	prefix, arithmetic := uint8(0), false
	for _, event := range p.events[:p.eventN] {
		if event&0x80 != 0 {
			break
		}
		n := p.nodes[event]
		switch {
		case n.op == 0x44:
			invariant[event] = true
		case n.op == 0x2b && p.stride(n.left) == 0:
			invariant[event] = true
		case n.op >= 0xa0 && n.op <= 0xa3 && invariant[n.left] && invariant[n.right]:
			invariant[event] = true
			arithmetic = true
		default:
			return p.finishInvariantPrefix(prefix, arithmetic, invariant)
		}
		prefix++
	}
	return p.finishInvariantPrefix(prefix, arithmetic, invariant)
}

func (p *regionLoopPlan) finishInvariantPrefix(prefix uint8, arithmetic bool, invariant [regionLoopMaxOps + 1]bool) (uint8, [regionLoopMaxOps + 1]bool) {
	var permanent [regionLoopMaxOps + 1]bool
	if prefix == 0 || prefix >= p.eventN || !arithmetic {
		return 0, permanent
	}
	mark := func(id uint8) {
		if invariant[id] && p.nodes[id].op != 0x20 {
			permanent[id] = true
		}
	}
	for _, event := range p.events[prefix:p.eventN] {
		if event&0x80 != 0 {
			mark(p.stores[event&0x7f].value)
			continue
		}
		n := p.nodes[event]
		if n.op >= 0xa0 && n.op <= 0xa3 {
			mark(n.left)
			mark(n.right)
		}
	}
	for _, l := range p.locals[:p.localN] {
		if l.written && l.typ == mtF64 {
			mark(l.value)
		}
	}
	roots := 0
	for _, keep := range permanent {
		if keep {
			roots++
		}
	}
	if roots == 0 || roots > 2 {
		return 0, [regionLoopMaxOps + 1]bool{}
	}
	return prefix, permanent
}
