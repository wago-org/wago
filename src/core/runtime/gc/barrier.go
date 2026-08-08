package gc

import "errors"

var errRange = errors.New("gc: index out of range")

type SlotKind uint8

type objectCard struct {
	handle uint32
	index  uint32 // inclusive payload-byte card-range start
	end    uint32 // inclusive payload-byte card-range end
	next   uint32 // one-based next card range for the same object
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

// WriteBarrierObject records an old-to-young edge when the caller cannot
// identify the mutated field. It conservatively dirties the complete payload;
// typed struct/array stores use writeBarrierObjectRange with the exact bytes.
func (c *Collector) WriteBarrierObject(parent Ref, child Ref) {
	if !parent.IsObj() || !child.IsObj() || !c.validObjectRef(parent) || !c.validObjectRef(child) {
		return
	}
	if c.cfg.Profile == ProfileTiny {
		c.tinyWriteBarrierObject(parent, child)
		return
	}
	h := handleOf(parent)
	e := &c.handles[h]
	if (e.space != spaceOld && e.space != spaceLarge) || e.young() || !c.entry(child).young() {
		return
	}
	c.remember(h)
	c.markWholeObjectCard(h)
}

// writeBarrierObjectRange is the post-write Throughput barrier for a known
// payload-byte interval. Collection is synchronous, so publishing the store
// before its card cannot race a collector.
func (c *Collector) writeBarrierObjectRange(parent Ref, child Ref, start, end uint32) {
	// Typed callers have already resolved the parent and validated the stored
	// child. Check parent generation before decoding the child so ordinary
	// nursery initialization exits with no duplicate handle-table work.
	if c.cfg.Profile == ProfileTiny {
		if child.IsObj() {
			c.tinyWriteBarrierObject(parent, child)
		}
		return
	}
	h := handleOf(parent)
	e := &c.handles[h]
	if (e.space != spaceOld && e.space != spaceLarge) || e.young() {
		return
	}
	if !child.IsObj() || !c.entry(child).young() {
		return
	}
	c.remember(h)
	if slot := e.cardSlot; slot != 0 && slotIndexOK(slot-1, len(c.objectCards)) {
		card := c.objectCards[slot-1]
		if card.handle == h && start >= card.index && end <= card.end {
			if c.telemetryEnabled() {
				c.cfg.Telemetry.pendingDuplicateDirties++
			}
			return
		}
	}
	c.addObjectCardRange(h, start, end)
}

// WriteBarrierRoot publishes a child stored in an exact externally walked root
// such as an off-heap native table descriptor. Throughput collections rescan
// external roots directly; Tiny must shade stores made during an incremental
// mark/remark/sweep cycle.
func (c *Collector) WriteBarrierRoot(child Ref) {
	if !child.IsObj() || !c.validObjectRef(child) || c.cfg.Profile != ProfileTiny {
		return
	}
	switch c.tinyGC.state {
	case tinyMark, tinyRemark:
		c.tinyMarkRef(child)
	case tinySweep:
		c.tinyMarkRefNow(child)
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
	if c.entry(child).young() {
		c.addSlotCard(kind, index)
	}
}
func (c *Collector) CardMarkArray(array Ref, elementIndex uint32) {
	if c.cfg.Profile == ProfileTiny || !array.IsObj() || !c.validObjectRef(array) {
		return
	}
	h := handleOf(array)
	e := &c.handles[h]
	if e.young() || (e.space != spaceOld && e.space != spaceLarge) {
		return
	}
	d, err := c.refDesc(array)
	if err != nil || d.Kind != KindArray || !d.ArrayElementsAreRefs() || elementIndex >= c.header(array).Aux {
		return
	}
	off := uint64(elementIndex) * uint64(d.ElemSize)
	if off > uint64(^uint32(0)) {
		return
	}
	c.remember(h)
	c.addObjectCardRange(h, uint32(off), uint32(off)+d.ElemSize-1)
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
	if !dst.IsObj() || !c.validObjectRef(dst) || length == 0 {
		return
	}
	if c.cfg.Profile == ProfileTiny {
		d, err := c.refDesc(dst)
		if err != nil || d.Kind != KindArray || !isCollectorRefKind(d.Elem) {
			return
		}
		for i := uint32(0); i < length; i++ {
			value, err := c.loadValue(dst, uint64(PayloadOffset)+uint64(start+i)*uint64(d.ElemSize), d.Elem)
			if err != nil {
				return
			}
			c.tinyWriteBarrierObject(dst, value.Ref)
		}
		return
	}
	h := handleOf(dst)
	e := &c.handles[h]
	sp := e.space
	if e.young() || (sp != spaceOld && sp != spaceLarge) {
		return
	}
	d, err := c.refDesc(dst)
	if err != nil || d.Kind != KindArray || !d.ArrayElementsAreRefs() || uint64(start)+uint64(length) > uint64(c.header(dst).Aux) {
		return
	}
	first := uint64(start) * uint64(d.ElemSize)
	last := (uint64(start)+uint64(length))*uint64(d.ElemSize) - 1
	if last > uint64(^uint32(0)) {
		return
	}
	// Bulk operations already traversed the source values. Dirty the destination
	// without a second mutator-side pass; collection decides whether each card is
	// useful while scanning it.
	c.remember(h)
	c.addObjectCardRange(h, uint32(first), uint32(last))
}

func (c *Collector) addObjectCard(h, payloadByte uint32) {
	c.addObjectCardRange(h, payloadByte, payloadByte)
}

func (c *Collector) addObjectCardRange(h, start, end uint32) {
	if h == 0 || int(h) >= len(c.handles) || end < start || c.cardBytes == 0 {
		return
	}
	e := &c.handles[h]
	if e.young() || (e.space != spaceOld && e.space != spaceLarge) {
		return
	}
	payloadBytes := uint32(0)
	if e.size > PayloadOffset {
		payloadBytes = e.size - PayloadOffset
	}
	if payloadBytes == 0 || start >= payloadBytes {
		return
	}
	if end >= payloadBytes {
		end = payloadBytes - 1
	}
	mask := c.cardBytes - 1
	start &^= mask
	end |= mask
	if end >= payloadBytes {
		end = payloadBytes - 1
	}

	// Ranges for one object form a short linked list. Dense writes expand one
	// range; sparse writes retain disjoint cards instead of widening across the
	// untouched middle of a large array.
	for slot := e.cardSlot; slot != 0; {
		if !slotIndexOK(slot-1, len(c.objectCards)) {
			return
		}
		pos := slot - 1
		card := &c.objectCards[pos]
		next := card.next
		if card.handle != h {
			return
		}
		adjacent := uint64(end)+1 >= uint64(card.index) && uint64(card.end)+1 >= uint64(start)
		if !adjacent {
			slot = next
			continue
		}
		duplicate := start >= card.index && end <= card.end
		if start < card.index {
			card.index = start
		}
		if end > card.end {
			card.end = end
		}
		if duplicate && c.telemetryEnabled() {
			c.cfg.Telemetry.pendingDuplicateDirties++
		}
		// Absorb any later ranges now bridged by this update. Tombstoned backing
		// entries are reclaimed when all card metadata is cleared after collection.
		link := &card.next
		for candidate := *link; candidate != 0; {
			candidatePos := candidate - 1
			if !slotIndexOK(candidatePos, len(c.objectCards)) {
				return
			}
			other := &c.objectCards[candidatePos]
			candidateNext := other.next
			if other.handle == h && uint64(card.end)+1 >= uint64(other.index) && uint64(other.end)+1 >= uint64(card.index) {
				if other.index < card.index {
					card.index = other.index
				}
				if other.end > card.end {
					card.end = other.end
				}
				*other = objectCard{}
				*link = candidateNext
				candidate = candidateNext
				continue
			}
			link = &other.next
			candidate = candidateNext
		}
		return
	}
	if injectFailure(c, failObjectCardGrowth) != nil {
		return
	}
	if c.telemetryEnabled() && len(c.objectCards) == cap(c.objectCards) {
		c.cfg.Telemetry.paths.CardGrowths++
	}
	c.objectCards = append(c.objectCards, objectCard{handle: h, index: start, end: end, next: e.cardSlot})
	e.cardSlot = uint32(len(c.objectCards))
	c.refreshNativeCards()
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

func (c *Collector) slotCardBits(kind SlotKind) *[]uint64 {
	switch kind {
	case SlotGlobal:
		return &c.globalCardBits
	case SlotTable:
		return &c.tableCardBits
	default:
		return nil
	}
}

func cardBitIsSet(bits []uint64, index uint32) bool {
	word := index >> 6
	return slotIndexOK(word, len(bits)) && bits[word]&(uint64(1)<<(index&63)) != 0
}

func (c *Collector) ensureSlotCardBit(kind SlotKind, index uint32) bool {
	bits := c.slotCardBits(kind)
	if bits == nil {
		return false
	}
	words := int(index>>6) + 1
	if len(*bits) < words {
		*bits = append(*bits, make([]uint64, words-len(*bits))...)
	}
	return true
}

func (c *Collector) addSlotCard(kind SlotKind, index uint32) {
	if !c.slotCardIndexOK(kind, index) || !c.ensureSlotCardBit(kind, index) {
		return
	}
	bits := c.slotCardBits(kind)
	word, bit := index>>6, uint64(1)<<(index&63)
	if (*bits)[word]&bit != 0 {
		if c.telemetryEnabled() {
			c.cfg.Telemetry.pendingDuplicateDirties++
		}
		return
	}
	if injectFailure(c, failSlotCardGrowth) != nil {
		// A correctness-preserving cold fallback scans every persistent slot at
		// the next minor collection when bounded metadata growth is unavailable.
		c.rootCardFallback = true
		return
	}
	if c.telemetryEnabled() && len(c.slotCards) == cap(c.slotCards) {
		c.cfg.Telemetry.paths.CardGrowths++
	}
	c.slotCards = append(c.slotCards, slotCard{kind: kind, index: index})
	(*bits)[word] |= bit
}
func (c *Collector) remember(h uint32) {
	if h == 0 || int(h) >= len(c.handles) {
		return
	}
	e := &c.handles[h]
	if e.remembered || e.young() || (e.space != spaceOld && e.space != spaceLarge) {
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
			c.removeCardsForHandle(h)
		}
	}
	clear(c.remembered[len(out):])
	c.remembered = out
}

// clearRememberedMetadata is valid after complete nursery evacuation: without
// nursery objects, no old-to-nursery edge can remain. It clears the dense-list
// metadata without rescanning the payload of every remembered parent.
func (c *Collector) clearRememberedMetadata() {
	for _, h := range c.remembered {
		if h != 0 && int(h) < len(c.handles) {
			c.handles[h].remembered = false
		}
	}
	clear(c.remembered)
	c.remembered = c.remembered[:0]
}
func (c *Collector) isNurseryRef(r Ref) bool {
	if !r.IsObj() || !c.validObjectRef(r) {
		return false
	}
	return c.entry(r).young()
}
func (c *Collector) removeCardsForHandle(h uint32) {
	if h == 0 || int(h) >= len(c.handles) {
		return
	}
	e := &c.handles[h]
	for slot, steps := e.cardSlot, 0; slot != 0 && steps <= len(c.objectCards); steps++ {
		if !slotIndexOK(slot-1, len(c.objectCards)) {
			break
		}
		pos := slot - 1
		next := c.objectCards[pos].next
		c.objectCards[pos] = objectCard{}
		slot = next
	}
	e.cardSlot = 0
	trimmed := false
	for len(c.objectCards) > 0 && c.objectCards[len(c.objectCards)-1].handle == 0 {
		last := len(c.objectCards) - 1
		c.objectCards = c.objectCards[:last]
		trimmed = true
	}
	if trimmed {
		c.refreshNativeCards()
	}
}
func (c *Collector) clearCardMetadata() {
	if c.telemetryEnabled() && c.cfg.Telemetry.active.active {
		c.cfg.Telemetry.active.cards.ClearedCards += c.dirtyObjectCardCount() + uint64(len(c.slotCards))
	}
	for _, card := range c.objectCards {
		if card.handle != 0 && int(card.handle) < len(c.handles) {
			c.handles[card.handle].cardSlot = 0
		}
	}
	for _, card := range c.slotCards {
		bits := c.slotCardBits(card.kind)
		if bits != nil && cardBitIsSet(*bits, card.index) {
			(*bits)[card.index>>6] &^= uint64(1) << (card.index & 63)
		}
	}
	clear(c.objectCards)
	clear(c.slotCards)
	c.objectCards = c.objectCards[:0]
	c.slotCards = c.slotCards[:0]
	c.rootCardFallback = false
	c.refreshNativeCards()
}

func (c *Collector) dirtyObjectCardCount() uint64 {
	var count uint64
	for _, card := range c.objectCards {
		if card.handle == 0 || card.end < card.index || c.cardBytes == 0 {
			continue
		}
		count += uint64(card.end/c.cardBytes-card.index/c.cardBytes) + 1
	}
	return count
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
		c.markWholeObjectCard(h)
	}
	return nil
}

func (c *Collector) markWholeObjectCard(h uint32) {
	if h == 0 || int(h) >= len(c.handles) {
		return
	}
	size := c.handles[h].size
	if size <= PayloadOffset {
		return
	}
	c.addObjectCardRange(h, 0, size-PayloadOffset-1)
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
		if c.entry(child).young() {
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
