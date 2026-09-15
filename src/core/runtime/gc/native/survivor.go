package gc

import "encoding/binary"

// Nursery entries do not use throughput size classes. Reuse the high class
// bits for bounded young-generation state without growing the native 20-byte
// handle entry. Large young objects retain their low class bits so they can age
// in place in throughput backing.
const (
	handleYoungBit       = uint16(1 << 15)
	handleAgeShift       = 13
	handleAgeMask        = uint16(3 << handleAgeShift)
	handleClassMask      = uint16((1 << handleAgeShift) - 1)
	maxTenuringThreshold = 3
)

func (e handleEntry) young() bool {
	return e.space == spaceNursery || (e.space == spaceLarge && e.class&handleYoungBit != 0)
}

func (e handleEntry) age() uint8 { return uint8((e.class & handleAgeMask) >> handleAgeShift) }

func (e *handleEntry) setYoungAge(age uint8) {
	if age > maxTenuringThreshold {
		age = maxTenuringThreshold
	}
	e.class = e.class&handleClassMask | handleYoungBit | uint16(age)<<handleAgeShift
}

func (e *handleEntry) clearYoungAge() { e.class &= handleClassMask }

func (c *Collector) isYoungHandle(h uint32) bool {
	return h != 0 && int(h) < len(c.handles) && c.handles[h].young()
}

func (c *Collector) edenBytes() uint32 { return c.cfg.NurseryBytes }

func (c *Collector) survivorBase(index uint8) uint32 {
	return align(c.edenBytes(), 16) + uint32(index&1)*c.survivorBytes
}

func (c *Collector) inEden(e handleEntry) bool {
	return e.space == spaceNursery && e.off < c.edenBytes()
}

func (c *Collector) inActiveSurvivor(e handleEntry) bool {
	if e.space != spaceNursery || c.survivorBytes == 0 {
		return false
	}
	base := c.survivorBase(c.survivorFrom)
	return e.off >= base && e.off < base+c.survivorBytes
}

func (c *Collector) tenureLargeInPlace(h uint32) {
	e := &c.handles[h]
	e.clearYoungAge()
	b := c.throughput.bytes(*e)
	flags := binary.LittleEndian.Uint32(b[12:16]) | FlagOld | FlagLarge
	binary.LittleEndian.PutUint32(b[12:16], flags)
}

func (c *Collector) ageLargeInPlace(h uint32, age uint8) {
	c.handles[h].setYoungAge(age)
}

func (c *Collector) poisonYoungSource(e handleEntry) {
	if !c.cfg.PoisonFreed || e.space != spaceNursery {
		return
	}
	for i := range c.nursery[e.off : e.off+e.size] {
		c.nursery[e.off+uint32(i)] = 0xdd
	}
}

func (c *Collector) removeYoungHandle(h uint32) {
	for i, candidate := range c.nurseryHandles {
		if candidate != h {
			continue
		}
		copy(c.nurseryHandles[i:], c.nurseryHandles[i+1:])
		last := len(c.nurseryHandles) - 1
		c.nurseryHandles[last] = 0
		c.nurseryHandles = c.nurseryHandles[:last]
		return
	}
}

func (c *Collector) currentYoungBytes() uint64 {
	var n uint64
	for _, h := range c.nurseryHandles {
		if c.isYoungHandle(h) {
			n += uint64(c.handles[h].size)
		}
	}
	return n
}

// adaptTenuring applies one bounded deterministic step after a successful
// minor collection. Optional pause targeting reads the clock only when the
// caller explicitly configured a nonzero target.
func (c *Collector) finishMinorCardMetadata() {
	if len(c.nurseryHandles) == 0 {
		c.clearRememberedMetadata()
		c.clearCardMetadata()
		return
	}
	out := c.remembered[:0]
	for _, h := range c.remembered {
		keep := h != 0 && int(h) < len(c.handles) && c.handles[h].remembered && !c.handles[h].young() &&
			(c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge)
		if keep && c.cardFallback {
			keep = c.handleContainsNurseryRef(h)
		} else if keep && c.handles[h].cardSlot != 0 {
			keep = c.objectCardsContainYoung(h)
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
	if c.cardFallback {
		return
	}
	slots := c.slotCards[:0]
	for _, card := range c.slotCards {
		var ref Ref
		switch card.kind {
		case SlotGlobal:
			if slotIndexOK(card.index, len(c.globalSlots)) {
				ref = c.globalSlots[card.index]
			}
		case SlotTable:
			if slotIndexOK(card.index, len(c.tableSlots)) {
				ref = c.tableSlots[card.index]
			}
		}
		if c.isNurseryRef(ref) {
			slots = append(slots, card)
			continue
		}
		if bits := c.slotCardBits(card.kind); bits != nil && cardBitIsSet(*bits, card.index) {
			(*bits)[card.index>>6] &^= uint64(1) << (card.index & 63)
		}
	}
	clear(c.slotCards[len(slots):])
	c.slotCards = slots
}

func (c *Collector) objectCardsContainYoung(h uint32) bool {
	for slot, steps := c.handles[h].cardSlot, 0; slot != 0 && steps <= len(c.objectCards); steps++ {
		if !slotIndexOK(slot-1, len(c.objectCards)) {
			return true
		}
		card := c.objectCards[slot-1]
		if card.handle == h && c.payloadRangeContainsYoung(h, card.index, card.end) {
			return true
		}
		slot = card.next
	}
	return false
}

func (c *Collector) payloadRangeContainsYoung(h, start, end uint32) bool {
	r := makeObjRef(h)
	d, err := c.refDesc(r)
	if err != nil || !d.HasRefs || end < start {
		return false
	}
	b := c.bytes(r)
	if d.Kind == KindStruct {
		for _, field := range d.Fields {
			if isCollectorRefKind(field.Kind) && field.Offset >= start && field.Offset <= end &&
				c.isNurseryRef(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+field.Offset:]))) {
				return true
			}
		}
		return false
	}
	if !d.ArrayElementsAreRefs() || d.ElemSize == 0 {
		return false
	}
	length := c.header(r).Aux
	first := start / d.ElemSize
	if start%d.ElemSize != 0 {
		first++
	}
	last := end / d.ElemSize
	if first >= length {
		return false
	}
	if last >= length {
		last = length - 1
	}
	for i := first; i <= last; i++ {
		if c.isNurseryRef(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+i*d.ElemSize:]))) {
			return true
		}
	}
	return false
}

func (c *Collector) adaptTenuring(copiedBytes, promotedBytes uint64, pauseNS uint64) {
	if c.cfg.DisableMovingNursery || c.survivorBytes == 0 {
		c.tenuringThreshold = 1
		return
	}
	threshold := c.tenuringThreshold
	if threshold < 1 {
		threshold = defaultTenuringThreshold
	}
	capacity := uint64(c.survivorBytes)
	occupancyHigh := capacity != 0 && copiedBytes*100 >= capacity*75
	occupancyLow := capacity == 0 || copiedBytes*100 <= capacity*50
	pauseHigh := c.cfg.MinorPauseTargetMicros != 0 && pauseNS > uint64(c.cfg.MinorPauseTargetMicros)*1000
	pauseLow := c.cfg.MinorPauseTargetMicros == 0 || pauseNS*2 < uint64(c.cfg.MinorPauseTargetMicros)*1000
	usedOld := uint64(c.throughput.bump) - c.throughput.freeBytes - c.throughput.pendingBytes
	oldPressure := c.throughput.limit != 0 && usedOld*100 >= uint64(c.throughput.limit)*70
	recentFull := c.stats.FullCollections != c.lastFullCollections

	switch {
	case occupancyHigh || pauseHigh:
		if threshold > 1 {
			threshold--
		}
	case occupancyLow && pauseLow && (oldPressure || recentFull || promotedBytes > copiedBytes):
		if threshold < maxTenuringThreshold {
			threshold++
		}
	}
	c.tenuringThreshold = threshold
	c.lastFullCollections = c.stats.FullCollections
}
