package gc

import "errors"

var errRange = errors.New("gc: index out of range")

type SlotKind uint8

type objectCard struct {
	handle uint32
	index  uint32
}

type slotCard struct {
	kind  SlotKind
	index uint32
}

const (
	SlotGlobal SlotKind = iota + 1
	SlotTable
	SlotFrame
)

// WriteBarrierObject records old-to-young object edges for generational
// collection and is the future hook for incremental tri-color marking.
func (c *Collector) WriteBarrierObject(parent Ref, child Ref) {
	if !parent.IsObj() || !child.IsObj() {
		return
	}
	if !c.validObjectRef(parent) || !c.validObjectRef(child) {
		return
	}
	if c.cfg.Profile == ProfileTiny {
		c.tinyWriteBarrierObject(parent, child)
		return
	}
	pe, ce := c.entry(parent), c.entry(child)
	if (pe.space == spaceOld || pe.space == spaceLarge) && ce.space == spaceNursery {
		c.remember(handleOf(parent))
	}
}

// WriteBarrierSlot records supported non-heap roots (globals/tables) that
// store young refs. Frame slots are intentionally unsupported until the runtime
// has exact frame-root metadata; frame refs must be supplied through RootSet.
func (c *Collector) WriteBarrierSlot(kind SlotKind, index uint32, child Ref) {
	if !child.IsObj() || !c.validObjectRef(child) {
		return
	}
	switch kind {
	case SlotGlobal:
		if !slotIndexOK(index, len(c.globalSlots)) {
			return
		}
	case SlotTable:
		if !slotIndexOK(index, len(c.tableSlots)) {
			return
		}
	case SlotFrame:
		return
	default:
		return
	}
	if c.cfg.Profile == ProfileTiny {
		if c.tinyGC.state == tinyMark || c.tinyGC.state == tinyRemark {
			c.tinyMarkRef(child)
		}
		return
	}
	if c.entry(child).space == spaceNursery {
		c.slotCards = append(c.slotCards, slotCard{kind: kind, index: index})
	}
}
func (c *Collector) CardMarkArray(array Ref, elementIndex uint32) {
	if array.IsObj() && c.validObjectRef(array) {
		c.objectCards = append(c.objectCards, objectCard{handle: handleOf(array), index: elementIndex})
	}
}
func (c *Collector) BulkWriteBarrier(dst Ref, start, length uint32) {
	if dst.IsObj() && c.validObjectRef(dst) && length != 0 {
		c.objectCards = append(c.objectCards, objectCard{handle: handleOf(dst), index: start})
		if length > 1 {
			end := uint64(start) + uint64(length) - 1
			if end > uint64(^uint32(0)) {
				end = uint64(^uint32(0))
			}
			c.objectCards = append(c.objectCards, objectCard{handle: handleOf(dst), index: uint32(end)})
		}
	}
}
func (c *Collector) remember(h uint32) {
	for _, x := range c.remembered {
		if x == h {
			return
		}
	}
	c.remembered = append(c.remembered, h)
}
func (c *Collector) removeRemembered(h uint32) {
	out := c.remembered[:0]
	for _, x := range c.remembered {
		if x != h {
			out = append(out, x)
		}
	}
	c.remembered = out
}
func (c *Collector) removeCardsForHandle(h uint32) {
	out := c.objectCards[:0]
	for _, card := range c.objectCards {
		if card.handle != h {
			out = append(out, card)
		}
	}
	c.objectCards = out
}
func (c *Collector) RememberedCount() int { return len(c.remembered) }
func (c *Collector) CardCount() int       { return len(c.objectCards) + len(c.slotCards) }
func (c *Collector) ForcePromote(r Ref) error {
	if !r.IsObj() {
		return errors.New("gc: not object")
	}
	if c.cfg.Profile == ProfileTiny {
		return nil
	}
	return c.promoteHandle(handleOf(r))
}

func (c *Collector) tinyWriteBarrierObject(parent Ref, child Ref) {
	if c.tinyGC.state != tinyMark && c.tinyGC.state != tinyRemark {
		return
	}
	ph, ch := handleOf(parent), handleOf(child)
	if ph == 0 || ch == 0 || int(ph) >= len(c.handles) || int(ch) >= len(c.handles) {
		return
	}
	if c.handles[ph].space != spaceTiny || c.handles[ch].space != spaceTiny {
		return
	}
	if c.tinyColorOf(ph) == tinyBlack && c.tinyColorOf(ch) == tinyWhite {
		// Hybrid Tiny barrier: gray the child (forward barrier) and re-gray the
		// parent (backward barrier). This is conservative and simple for the first
		// non-moving incremental policy; repeated container writes remain safe.
		c.tinyGrayHandle(ch)
		c.tinyGrayHandle(ph)
	}
}
