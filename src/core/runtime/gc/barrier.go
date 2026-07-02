package gc

import (
	"encoding/binary"
	"errors"
)

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
	if c.cfg.Profile == ProfileTiny {
		return
	}
	if array.IsObj() && c.validObjectRef(array) {
		c.addObjectCard(handleOf(array), elementIndex)
	}
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
	c.addObjectCard(h, start)
	if length > 1 {
		end := uint64(start) + uint64(length) - 1
		if end > uint64(^uint32(0)) {
			end = uint64(^uint32(0))
		}
		c.addObjectCard(h, uint32(end))
	}
	c.rememberBulkArrayRange(h, start, length)
}

func (c *Collector) rememberBulkArrayRange(h, start, length uint32) {
	if h == 0 || int(h) >= len(c.handles) {
		return
	}
	sp := c.handles[h].space
	if sp != spaceOld && sp != spaceLarge {
		return
	}
	dst := makeObjRef(h)
	hdr := c.header(dst)
	d, err := c.desc(TypeID(hdr.TypeID))
	if err != nil || !d.ArrayElementsAreRefs() || start >= hdr.Aux {
		return
	}
	end := uint64(start) + uint64(length)
	if end > uint64(hdr.Aux) {
		end = uint64(hdr.Aux)
	}
	b := c.bytes(dst)
	for i := start; uint64(i) < end; i++ {
		off := PayloadOffset + i*d.ElemSize
		if uint64(off)+4 > uint64(len(b)) {
			return
		}
		if c.isNurseryRef(Ref(binary.LittleEndian.Uint32(b[off:]))) {
			c.remember(h)
			return
		}
	}
}
func (c *Collector) addObjectCard(h, index uint32) {
	for _, card := range c.objectCards {
		if card.handle == h && card.index == index {
			return
		}
	}
	c.objectCards = append(c.objectCards, objectCard{handle: h, index: index})
}
func (c *Collector) addSlotCard(kind SlotKind, index uint32) {
	for _, card := range c.slotCards {
		if card.kind == kind && card.index == index {
			return
		}
	}
	c.slotCards = append(c.slotCards, slotCard{kind: kind, index: index})
}
func (c *Collector) pruneSlotCard(kind SlotKind, index uint32) {
	out := c.slotCards[:0]
	for _, card := range c.slotCards {
		if card.kind != kind || card.index != index {
			out = append(out, card)
		}
	}
	c.slotCards = out
}
func (c *Collector) pruneSlotCardUnlessNursery(kind SlotKind, index uint32, r Ref) {
	if r.IsObj() && c.validObjectRef(r) && c.entry(r).space == spaceNursery {
		return
	}
	c.pruneSlotCard(kind, index)
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
func (c *Collector) pruneRemembered() {
	out := c.remembered[:0]
	for _, h := range c.remembered {
		if h == 0 || int(h) >= len(c.handles) {
			continue
		}
		sp := c.handles[h].space
		if (sp == spaceOld || sp == spaceLarge) && c.handleContainsNurseryRef(h) {
			out = append(out, h)
		}
	}
	c.remembered = out
}
func (c *Collector) pruneRememberedHandleUnlessNursery(h uint32) {
	if h == 0 || int(h) >= len(c.handles) {
		return
	}
	sp := c.handles[h].space
	if (sp == spaceOld || sp == spaceLarge) && !c.handleContainsNurseryRef(h) {
		c.removeRemembered(h)
	}
}
func (c *Collector) isNurseryRef(r Ref) bool {
	if !r.IsObj() || !c.validObjectRef(r) {
		return false
	}
	return c.entry(r).space == spaceNursery
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
