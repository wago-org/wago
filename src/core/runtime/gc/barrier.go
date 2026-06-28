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
	pe, ce := c.entry(parent), c.entry(child)
	if pe.space == spaceOld && ce.space == spaceNursery {
		c.remember(handleOf(parent))
	}
}

// WriteBarrierSlot records non-heap roots (globals/tables/frames) that store
// young refs. Slot barriers let minor collection scan root-like locations not
// otherwise visible in the current exact RootSet.
func (c *Collector) WriteBarrierSlot(kind SlotKind, index uint32, child Ref) {
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
func (c *Collector) RememberedCount() int { return len(c.remembered) }
func (c *Collector) CardCount() int       { return len(c.cards) }
func (c *Collector) ForcePromote(r Ref) error {
	if !r.IsObj() {
		return errors.New("gc: not object")
	}
	return c.promoteHandle(handleOf(r))
}
