//go:build amd64

package amd64

import "os"

var scalarMemoryRecurrenceEnabled = os.Getenv("WAGO_AMD64_SCALAR_MEMORY_RECURRENCE") == "1"

// Admit up to two invariant output cells whose old values are read exactly
// once before their updates. Their direct snapshots must die by those updates.
// Every other access remains ordered. Runtime range and strict alias guards
// prove that postponing these stores cannot be observed on the fast path.
func (p *regionLoopPlan) scalarMemoryRecurrence() bool {
	var selected [2]uint8
	count := 0
	for i, store := range p.stores[:p.storeN] {
		if p.stride(store.address) != 0 {
			continue
		}
		address := p.nodes[store.address]
		id := uint8(0)
		for _, load := range p.loads[:p.loadN] {
			n := p.nodes[load.node]
			a := p.nodes[n.left]
			if a.coeff == address.coeff && a.bits == address.bits && n.bits == store.offset {
				if id != 0 {
					return false
				}
				id = load.node
			}
		}
		if id == 0 {
			continue
		}
		if p.nodes[id].pos >= store.pos {
			return false
		}
		// Two store events to the same cell need an explicit version contract.
		for j, other := range p.stores[:p.storeN] {
			a := p.nodes[other.address]
			if i != j && a.coeff == address.coeff && a.bits == address.bits && other.offset == store.offset {
				return false
			}
		}
		var depends [regionLoopMaxOps + 1]bool
		depends[id] = true
		for n := uint8(1); n <= p.nodeN; n++ {
			node := p.nodes[n]
			if node.op >= 0xa0 && node.op <= 0xa3 {
				depends[n] = depends[node.left] || depends[node.right]
			}
		}
		root := p.nodes[store.value]
		if !depends[store.value] || root.op < 0xa0 || root.op > 0xa3 {
			return false
		}
		after := false
		for _, event := range p.events[:p.eventN] {
			if event == 0x80|uint8(i) {
				after = true
				continue
			}
			if !after {
				continue
			}
			if event&0x80 != 0 {
				if p.stores[event&0x7f].value == id {
					return false
				}
				continue
			}
			n := p.nodes[event]
			if n.op >= 0xa0 && n.op <= 0xa3 && (n.left == id || n.right == id) {
				return false
			}
		}
		for _, l := range p.locals[:p.localN] {
			if l.typ == mtF64 && l.written && l.value == id {
				return false
			}
		}
		selected[i] = id
		count++
	}
	if count == 0 {
		return false
	}
	p.scalar = true
	p.reductionLoad = selected
	return true
}

// Load the initial cell values after all guards; every later iteration reads
// their private register homes. This model retains permanent-register demand.
func (p *regionLoopPlan) prepareMemoryRecurrence() (uint8, [regionLoopMaxOps + 1]bool) {
	var keep [regionLoopMaxOps + 1]bool
	var events [regionLoopMaxOps]uint8
	prefix := uint8(0)
	for _, id := range p.reductionLoad {
		if id != 0 {
			events[prefix] = id
			prefix++
			keep[id] = true
		}
	}
	at := int(prefix)
	for _, id := range p.events[:p.eventN] {
		if id&0x80 == 0 && keep[id] {
			continue
		}
		events[at] = id
		at++
	}
	p.events = events
	return prefix, keep
}
