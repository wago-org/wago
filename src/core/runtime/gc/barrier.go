package gc

import "errors"

var errRange = errors.New("gc: index out of range")

type runtimeBarrierState uint8

const (
	runtimeBarrierNoBarrier runtimeBarrierState = iota
	runtimeBarrierYoungParent
	runtimeBarrierKnownOldChild
	runtimeBarrierExistingCard
	runtimeBarrierCardMark
	runtimeBarrierSlowBarrier
)

func (c *Collector) noteBarrierState(state runtimeBarrierState) {
	if !c.telemetryEnabled() {
		return
	}
	b := &c.cfg.Telemetry.barriers
	switch state {
	case runtimeBarrierNoBarrier:
		b.NoBarrier++
	case runtimeBarrierYoungParent:
		b.YoungParent++
	case runtimeBarrierKnownOldChild:
		b.KnownOldChild++
	case runtimeBarrierExistingCard:
		b.ExistingCard++
	case runtimeBarrierCardMark:
		b.CardMark++
	case runtimeBarrierSlowBarrier:
		b.SlowBarrier++
	}
}

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
		c.noteBarrierState(runtimeBarrierSlowBarrier)
		c.tinyWriteBarrierObject(parent, child)
		return
	}
	h := handleOf(parent)
	e := &c.handles[h]
	if (e.space != spaceOld && e.space != spaceLarge) || e.young() {
		c.noteBarrierState(runtimeBarrierYoungParent)
		return
	}
	if !c.entry(child).young() {
		c.noteBarrierState(runtimeBarrierKnownOldChild)
		return
	}
	payloadEnd := uint32(0)
	if e.size > PayloadOffset {
		payloadEnd = e.size - PayloadOffset - 1
	}
	if c.telemetryEnabled() {
		c.noteBarrierState(c.classifyObjectCardRange(h, 0, payloadEnd))
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
			c.noteBarrierState(runtimeBarrierSlowBarrier)
			c.tinyWriteBarrierObject(parent, child)
		} else {
			c.noteBarrierState(runtimeBarrierNoBarrier)
		}
		return
	}
	h := handleOf(parent)
	e := &c.handles[h]
	if (e.space != spaceOld && e.space != spaceLarge) || e.young() {
		c.noteBarrierState(runtimeBarrierYoungParent)
		return
	}
	if !child.IsObj() {
		c.noteBarrierState(runtimeBarrierNoBarrier)
		return
	}
	if !c.entry(child).young() {
		c.noteBarrierState(runtimeBarrierKnownOldChild)
		return
	}
	if c.telemetryEnabled() {
		c.noteBarrierState(c.classifyObjectCardRange(h, start, end))
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
// external roots directly; Tiny shades stores made during an incremental cycle.
// During sweep, the source graph must have remained in the exact roots until the
// store; a one-pass sweep cannot resurrect descendants reclaimed earlier.
func (c *Collector) WriteBarrierRoot(child Ref) {
	if !child.IsObj() || !c.validObjectRef(child) || c.cfg.Profile != ProfileTiny {
		return
	}
	switch c.tinyGC.state {
	case tinyMark, tinyRemark:
		c.tinyMarkRef(child)
	case tinySweep:
		c.tinyMarkSweepRef(child)
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
			// Root stores during sweep pause the sweep cursor and enqueue bounded
			// mark work. The source graph must still be retained by exact roots until
			// publication: descendants reclaimed at earlier indexes cannot be revived.
			c.tinyMarkSweepRef(child)
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
		// Validate the complete range with widened arithmetic before deriving any
		// per-element index. In particular, never let start+base+i wrap in uint32.
		if uint64(start)+uint64(length) > uint64(c.header(dst).Aux) {
			return
		}
		c.noteBarrierState(runtimeBarrierSlowBarrier)
		const tinyBulkBarrierChunk = uint32(64)
		for base := uint32(0); base < length; {
			n := length - base
			if n > tinyBulkBarrierChunk {
				n = tinyBulkBarrierChunk
			}
			for i := uint32(0); i < n; i++ {
				index := uint64(start) + uint64(base) + uint64(i)
				value, err := c.loadValue(dst, uint64(PayloadOffset)+index*uint64(d.ElemSize), d.Elem)
				if err != nil {
					return
				}
				c.tinyWriteBarrierObject(dst, value.Ref)
			}
			if c.tinyGC.state == tinyMark || c.tinyGC.state == tinyRemark {
				if c.tinyGC.telemetryOwned {
					c.cfg.Telemetry.resume()
					c.cfg.Telemetry.setPhase(telemetryPhaseMarking)
				}
				c.tinyDrainGrayBudget(tinyStepObjectScanBudget)
				if c.tinyGC.telemetryOwned {
					c.cfg.Telemetry.suspend()
				}
			}
			base += n
		}
		return
	}
	h := handleOf(dst)
	e := &c.handles[h]
	sp := e.space
	if e.young() || (sp != spaceOld && sp != spaceLarge) {
		c.noteBarrierState(runtimeBarrierYoungParent)
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
	if c.telemetryEnabled() {
		c.noteBarrierState(c.classifyObjectCardRange(h, uint32(first), uint32(last)))
	}
	c.remember(h)
	c.addObjectCardRange(h, uint32(first), uint32(last))
}

func (c *Collector) addObjectCard(h, payloadByte uint32) {
	c.addObjectCardRange(h, payloadByte, payloadByte)
}

func (c *Collector) classifyObjectCardRange(h, start, end uint32) runtimeBarrierState {
	if h == 0 || int(h) >= len(c.handles) || end < start || c.cardBytes == 0 {
		return runtimeBarrierSlowBarrier
	}
	e := &c.handles[h]
	payloadBytes := uint32(0)
	if e.size > PayloadOffset {
		payloadBytes = e.size - PayloadOffset
	}
	if payloadBytes == 0 || start >= payloadBytes {
		return runtimeBarrierSlowBarrier
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
	for slot := e.cardSlot; slot != 0; {
		if !slotIndexOK(slot-1, len(c.objectCards)) {
			return runtimeBarrierSlowBarrier
		}
		card := c.objectCards[slot-1]
		if card.handle != h {
			return runtimeBarrierSlowBarrier
		}
		if start >= card.index && end <= card.end {
			return runtimeBarrierExistingCard
		}
		if uint64(end)+1 >= uint64(card.index) && uint64(card.end)+1 >= uint64(start) {
			return runtimeBarrierCardMark
		}
		slot = card.next
	}
	return runtimeBarrierSlowBarrier
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
	// untouched middle of a large array. Move a matched range's interval to the
	// head slot so repeated writes return through both Go and native head checks.
	for slot := e.cardSlot; slot != 0; {
		if !slotIndexOK(slot-1, len(c.objectCards)) {
			goto fallback
		}
		pos := slot - 1
		card := &c.objectCards[pos]
		next := card.next
		if card.handle != h {
			goto fallback
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
		candidateLink := &card.next
		for candidate := *candidateLink; candidate != 0; {
			candidatePos := candidate - 1
			if !slotIndexOK(candidatePos, len(c.objectCards)) {
				goto fallback
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
				c.releaseObjectCardSlot(candidatePos)
				*candidateLink = candidateNext
				candidate = candidateNext
				continue
			}
			candidateLink = &other.next
			candidate = candidateNext
		}
		if slot != e.cardSlot {
			head := &c.objectCards[e.cardSlot-1]
			head.index, card.index = card.index, head.index
			head.end, card.end = card.end, head.end
		}
		return
	}
	{
		card := objectCard{handle: h, index: start, end: end, next: e.cardSlot}
		if slot := c.freeObjectCardSlot; slot != 0 {
			if !slotIndexOK(slot-1, len(c.objectCards)) {
				goto fallback
			}
			free := &c.objectCards[slot-1]
			if free.handle != 0 || free.index != 0 || free.end != 0 || (free.next != 0 && !slotIndexOK(free.next-1, len(c.objectCards))) {
				goto fallback
			}
			c.freeObjectCardSlot = free.next
			*free = card
			e.cardSlot = slot
			return
		}
		if injectFailure(c, failObjectCardGrowth) != nil {
			goto fallback
		}
		if c.telemetryEnabled() && len(c.objectCards) == cap(c.objectCards) {
			c.cfg.Telemetry.paths.CardGrowths++
		}
		c.objectCards = append(c.objectCards, card)
		e.cardSlot = uint32(len(c.objectCards))
		c.refreshNativeCards()
		return
	}

fallback:
	// If another exact range exists, make it conservative. Otherwise keep a
	// collector-wide cold fallback active so later successful card additions
	// cannot hide the already-published edge.
	if slot := e.cardSlot; slot != 0 && slotIndexOK(slot-1, len(c.objectCards)) {
		card := &c.objectCards[slot-1]
		if card.handle == h {
			card.index = 0
			card.end = payloadBytes - 1
			return
		}
	}
	if e.remembered {
		c.cardFallback = true
	}
}

func (c *Collector) releaseObjectCardSlot(pos uint32) {
	if !slotIndexOK(pos, len(c.objectCards)) || c.objectCards[pos].handle == 0 {
		return
	}
	c.objectCards[pos] = objectCard{next: c.freeObjectCardSlot}
	c.freeObjectCardSlot = pos + 1
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
		c.cardFallback = true
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
	if e.cardSlot == 0 {
		return
	}
	for slot, steps := e.cardSlot, 0; slot != 0 && steps <= len(c.objectCards); steps++ {
		if !slotIndexOK(slot-1, len(c.objectCards)) {
			break
		}
		pos := slot - 1
		next := c.objectCards[pos].next
		c.releaseObjectCardSlot(pos)
		slot = next
	}
	e.cardSlot = 0
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
	c.freeObjectCardSlot = 0
	c.slotCards = c.slotCards[:0]
	c.cardFallback = false
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
func (c *Collector) CardCount() int {
	count := len(c.slotCards)
	for i := range c.objectCards {
		if c.objectCards[i].handle != 0 {
			count++
		}
	}
	return count
}
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
	if !c.tinyIsWhite(ch) {
		return
	}
	c.tinyWriteBarrierWhiteChild(ph, ch, child)
}

//go:noinline
func (c *Collector) tinyWriteBarrierWhiteChild(ph, ch uint32, child Ref) {
	black := tinyMarkState(c.tinyGC.markEpoch)
	switch c.tinyGC.color[ph] {
	case black | tinyMarkGrayBit:
		// A partially scanned gray parent may be mutated before its cursor. Shade
		// the new child immediately; writes after the cursor are conservatively
		// shaded too, avoiding cursor-position checks in the mutator barrier.
		c.tinyQueueGrayHandle(ch)
	case black:
		if c.tinyGC.state == tinySweep {
			c.tinyMarkSweepRef(child)
			return
		}
		// Preserve the existing hybrid policy for black parents: shade the child
		// and re-gray the parent. Broader barrier simplification remains later
		// #319 work.
		c.tinyQueueGrayHandle(ch)
		c.tinyQueueGrayHandle(ph)
	}
}
