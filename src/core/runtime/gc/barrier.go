package gc

import "errors"

var errRange = errors.New("gc: index out of range")

type SlotKind uint8

type objectCard struct {
	handle uint32
	index  uint32 // inclusive dirty-range start
	end    uint32 // inclusive dirty-range end
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
		switch c.tinyGC.state {
		case tinyMark, tinyRemark:
			c.tinyMarkRef(child)
		case tinySweep:
			// Root stores during sweep publish a new root after the remark root
			// snapshot. Mark and drain it immediately so the remaining sweep cannot
			// reclaim the newly rooted object or its children.
			c.tinyMarkRefNow(child)
		}
		return
	}
	if c.entry(child).space == spaceNursery {
		c.addSlotCard(kind, index)
	}
}
func (c *Collector) CardMarkArray(array Ref, elementIndex uint32) {
	if c.cfg.Profile == ProfileTiny || !array.IsObj() || !c.validObjectRef(array) {
		return
	}
	e := c.entry(array)
	if e.space != spaceOld && e.space != spaceLarge {
		return
	}
	c.addObjectCard(handleOf(array), elementIndex)
}

// BulkWriteBarrier records dirty array range metadata after a bulk ref-array
// write. It is a post-write barrier: callers must store or copy refs into the
// destination range before invoking it. Calling this before the writes is not
// sufficient to preserve newly written nursery refs behind old or large arrays.
func (c *Collector) BulkWriteBarrier(dst Ref, start, length uint32) {
	c.PostBulkWriteBarrier(dst, start, length)
}

// PostBulkWriteBarrier records dirty array range metadata after a bulk ref-array
// write. Callers must invoke it only after the destination range contains the
// new refs so the remembered-set scan can observe old/large-to-nursery edges.
func (c *Collector) PostBulkWriteBarrier(dst Ref, start, length uint32) {
	if c.cfg.Profile == ProfileTiny {
		return
	}
	if !dst.IsObj() || !c.validObjectRef(dst) || length == 0 {
		return
	}
	h := handleOf(dst)
	sp := c.handles[h].space
	if sp != spaceOld && sp != spaceLarge {
		return
	}
	end := uint64(start) + uint64(length) - 1
	if end > uint64(^uint32(0)) {
		end = uint64(^uint32(0))
	}
	c.addObjectCardRange(h, start, uint32(end))
	// Remember conservatively when the newly written range contains a nursery
	// edge. Do not scan unrelated elements or remove an existing remembered bit
	// on this hot path; collection-time pruning establishes the exact cold state.
	if c.arrayRangeContainsNurseryRef(dst, start, length) {
		c.remember(h)
	}
}

func (c *Collector) addObjectCard(h, index uint32) { c.addObjectCardRange(h, index, index) }

func (c *Collector) addObjectCardRange(h, start, end uint32) {
	if h == 0 || int(h) >= len(c.handles) || end < start {
		return
	}
	e := &c.handles[h]
	if e.space != spaceOld && e.space != spaceLarge {
		return
	}
	if e.cardSlot != 0 {
		card := &c.objectCards[e.cardSlot-1]
		if start < card.index {
			card.index = start
		}
		if end > card.end {
			card.end = end
		}
		return
	}
	c.objectCards = append(c.objectCards, objectCard{handle: h, index: start, end: end})
	e.cardSlot = uint32(len(c.objectCards))
}

func slotCardKey(kind SlotKind, index uint32) uint64 {
	return uint64(kind)<<32 | uint64(index)
}

func (c *Collector) slotCardIndexOK(kind SlotKind, index uint32) bool {
	switch kind {
	case SlotGlobal:
		return slotIndexOK(index, len(c.globalSlots))
	case SlotTable:
		return slotIndexOK(index, len(c.tableSlots))
	default:
		return false
	}
}

func (c *Collector) addSlotCard(kind SlotKind, index uint32) {
	if !c.slotCardIndexOK(kind, index) {
		return
	}
	key := slotCardKey(kind, index)
	if c.slotCardSlot != nil && c.slotCardSlot[key] != 0 {
		return
	}
	if c.slotCardSlot == nil {
		c.slotCardSlot = make(map[uint64]uint32)
	}
	c.slotCards = append(c.slotCards, slotCard{kind: kind, index: index})
	c.slotCardSlot[key] = uint32(len(c.slotCards))
}
func (c *Collector) pruneSlotCard(kind SlotKind, index uint32) {
	key := slotCardKey(kind, index)
	slot := c.slotCardSlot[key]
	if slot == 0 {
		return
	}
	pos := int(slot - 1)
	last := len(c.slotCards) - 1
	moved := c.slotCards[last]
	c.slotCards[pos] = moved
	c.slotCards = c.slotCards[:last]
	delete(c.slotCardSlot, key)
	if pos != last {
		c.slotCardSlot[slotCardKey(moved.kind, moved.index)] = uint32(pos + 1)
	}
}
func (c *Collector) pruneSlotCardUnlessNursery(kind SlotKind, index uint32, r Ref) {
	if r.IsObj() && c.validObjectRef(r) && c.entry(r).space == spaceNursery {
		return
	}
	c.pruneSlotCard(kind, index)
}
func (c *Collector) remember(h uint32) {
	if h == 0 || int(h) >= len(c.handles) {
		return
	}
	e := &c.handles[h]
	if e.remembered || (e.space != spaceOld && e.space != spaceLarge) {
		return
	}
	e.remembered = true
	c.remembered = append(c.remembered, h)
}
func (c *Collector) removeRemembered(h uint32) {
	if h != 0 && int(h) < len(c.handles) {
		// The dense list is compacted once on the collection cold path. free is
		// called only during that sweep, so a cleared handle cannot be reused
		// before pruneRemembered removes its stale list entry.
		c.handles[h].remembered = false
	}
}
func (c *Collector) pruneRemembered() {
	out := c.remembered[:0]
	for _, h := range c.remembered {
		keep := h != 0 && int(h) < len(c.handles) && c.handles[h].remembered
		if keep {
			sp := c.handles[h].space
			keep = (sp == spaceOld || sp == spaceLarge) && c.handleContainsNurseryRef(h)
		}
		if keep {
			out = append(out, h)
		} else if h != 0 && int(h) < len(c.handles) {
			c.handles[h].remembered = false
		}
	}
	clear(c.remembered[len(out):])
	c.remembered = out
}
func (c *Collector) isNurseryRef(r Ref) bool {
	if !r.IsObj() || !c.validObjectRef(r) {
		return false
	}
	return c.entry(r).space == spaceNursery
}
func (c *Collector) removeCardsForHandle(h uint32) {
	if h == 0 || int(h) >= len(c.handles) {
		return
	}
	e := &c.handles[h]
	if e.cardSlot == 0 {
		return
	}
	pos := int(e.cardSlot - 1)
	last := len(c.objectCards) - 1
	moved := c.objectCards[last]
	c.objectCards[pos] = moved
	c.objectCards = c.objectCards[:last]
	e.cardSlot = 0
	if pos != last && moved.handle != 0 && int(moved.handle) < len(c.handles) {
		c.handles[moved.handle].cardSlot = uint32(pos + 1)
	}
}
func (c *Collector) clearCardMetadata() {
	for _, card := range c.objectCards {
		if card.handle != 0 && int(card.handle) < len(c.handles) {
			c.handles[card.handle].cardSlot = 0
		}
	}
	c.objectCards = c.objectCards[:0]
	c.slotCards = c.slotCards[:0]
	clear(c.slotCardSlot)
}

func (c *Collector) RememberedCount() int { return len(c.remembered) }
func (c *Collector) CardCount() int       { return len(c.objectCards) + len(c.slotCards) }
func (c *Collector) ForcePromote(r Ref) error {
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if !r.IsObj() {
		return errors.New("gc: not object")
	}
	if !c.validObjectRef(r) {
		return errors.New("gc: invalid object ref")
	}
	if c.cfg.Profile == ProfileTiny {
		return nil
	}
	h := handleOf(r)
	if err := c.promoteHandle(h); err != nil {
		return err
	}
	if c.handleContainsNurseryRef(h) {
		c.remember(h)
	}
	return nil
}

func (c *Collector) arrayRangeContainsNurseryRef(array Ref, start, length uint32) bool {
	d, err := c.refDesc(array)
	if err != nil || !d.ArrayElementsAreRefs() || length == 0 {
		return false
	}
	arrayLen := c.header(array).Aux
	if uint64(start)+uint64(length) > uint64(arrayLen) {
		return false
	}
	b := c.bytes(array)
	off := uint64(PayloadOffset) + uint64(start)*uint64(d.ElemSize)
	for i := uint32(0); i < length; i++ {
		r := Ref(uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24)
		if c.isNurseryRef(r) {
			return true
		}
		off += uint64(d.ElemSize)
	}
	return false
}

func (c *Collector) handleContainsNurseryRef(h uint32) bool {
	if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
		return false
	}
	found := false
	c.scanObjectRefs(h, func(child Ref) {
		if found || !child.IsObj() || !c.validObjectRef(child) {
			return
		}
		if c.entry(child).space == spaceNursery {
			found = true
		}
	})
	return found
}

func (c *Collector) tinyWriteBarrierObject(parent Ref, child Ref) {
	if c.tinyGC.state != tinyMark && c.tinyGC.state != tinyRemark && c.tinyGC.state != tinySweep {
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
		if c.tinyGC.state == tinySweep {
			c.tinyMarkRefNow(child)
			return
		}
		// Hybrid Tiny barrier: gray the child (forward barrier) and re-gray the
		// parent (backward barrier). This is conservative and simple for the first
		// non-moving incremental policy; repeated container writes remain safe.
		c.tinyGrayHandle(ch)
		c.tinyGrayHandle(ph)
	}
}
