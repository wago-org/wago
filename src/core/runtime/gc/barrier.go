package gc

import "errors"

var errRange = errors.New("gc: index out of range")

type SlotKind uint8

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
	if c.cfg.Profile == ProfileTiny {
		c.tinyWriteBarrierObject(parent, child)
		return
	}
	pe, ce := c.entry(parent), c.entry(child)
	if pe.space == spaceOld && ce.space == spaceNursery {
		c.remember(handleOf(parent))
	}
}

// WriteBarrierSlot records non-heap roots (globals/tables/frames) that store
// young refs. Slot barriers let minor collection scan root-like locations not
// otherwise visible in the current exact RootSet.
func (c *Collector) WriteBarrierSlot(kind SlotKind, index uint32, child Ref) {
	if c.cfg.Profile == ProfileTiny {
		if child.IsObj() && c.tinyGC.state == tinyMark {
			c.tinyMarkRef(child)
		}
		return
	}
	if child.IsObj() && c.entry(child).space == spaceNursery {
		c.cards = append(c.cards, uint32(kind)<<24|index)
	}
}
func (c *Collector) CardMarkArray(array Ref, elementIndex uint32) {
	if array.IsObj() {
		c.cards = append(c.cards, handleOf(array)<<16|(elementIndex&0xffff))
	}
}
func (c *Collector) BulkWriteBarrier(dst Ref, start, length uint32) {
	if dst.IsObj() {
		c.cards = append(c.cards, handleOf(dst)<<16|(start&0xffff))
		if length > 1 {
			c.cards = append(c.cards, handleOf(dst)<<16|((start+length-1)&0xffff))
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
	out := c.cards[:0]
	for _, card := range c.cards {
		if card>>16 != h {
			out = append(out, card)
		}
	}
	c.cards = out
}
func (c *Collector) RememberedCount() int { return len(c.remembered) }
func (c *Collector) CardCount() int       { return len(c.cards) }
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
	if c.tinyGC.state != tinyMark {
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
