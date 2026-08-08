package gc

import (
	"encoding/binary"
	"time"
)

// scanRememberedCards traces only the dirty payload cards for one old/large
// object. The handle's cardSlot is the head of a short linked list of disjoint,
// coalesced byte ranges; clean cards and clean old objects are never visited.
func (c *Collector) scanRememberedCards(h uint32) {
	if h == 0 || int(h) >= len(c.handles) {
		return
	}
	e := &c.handles[h]
	if e.young() || (e.space != spaceOld && e.space != spaceLarge) {
		return
	}
	if e.cardSlot == 0 {
		// Metadata growth failure is fail-safe: remembered membership remains
		// authoritative and falls back to one complete object scan.
		c.scanObjectRefs(h, c.markNurseryRef)
		return
	}
	startTime := time.Time{}
	if c.telemetryEnabled() {
		startTime = c.cfg.Telemetry.scanStart()
	}
	var payloadBytes, slots, dirtyCards, usefulCards uint64
	for slot, steps := e.cardSlot, 0; slot != 0 && steps <= len(c.objectCards); steps++ {
		if !slotIndexOK(slot-1, len(c.objectCards)) {
			break
		}
		card := c.objectCards[slot-1]
		slot = card.next
		if card.handle != h || card.end < card.index {
			continue
		}
		n, useful := c.scanObjectPayloadRange(h, card.index, card.end)
		slots += uint64(n)
		payloadBytes += uint64(card.end-card.index) + 1
		dirtyCards += uint64(card.end/c.cardBytes-card.index/c.cardBytes) + 1
		usefulCards += uint64(useful)
	}
	if c.telemetryEnabled() {
		whole := payloadBytes >= uint64(e.size-PayloadOffset)
		c.cfg.Telemetry.noteCardScan(startTime, payloadBytes, slots, dirtyCards, usefulCards, whole)
	}
}

// scanObjectPayloadRange visits reference slots whose payload-relative starts
// lie in [start,end]. One coalesced range is scanned in one descriptor walk;
// useful-card accounting advances fixed boundaries without one call per card.
func (c *Collector) scanObjectPayloadRange(h, start, end uint32) (slots, usefulCards uint32) {
	r := makeObjRef(h)
	d, err := c.refDesc(r)
	if err != nil || !d.HasRefs || end < start {
		return 0, 0
	}
	b := c.bytes(r)
	if d.Kind == KindStruct {
		lastCard := ^uint32(0)
		cardUseful := false
		for _, field := range d.Fields {
			if !isCollectorRefKind(field.Kind) || field.Offset < start || field.Offset > end {
				continue
			}
			card := field.Offset / c.cardBytes
			if card != lastCard {
				if cardUseful {
					usefulCards++
				}
				lastCard, cardUseful = card, false
			}
			slots++
			child := Ref(binary.LittleEndian.Uint32(b[PayloadOffset+field.Offset:]))
			if c.isNurseryRef(child) {
				cardUseful = true
			}
			c.markNurseryRef(child)
		}
		if cardUseful {
			usefulCards++
		}
		return slots, usefulCards
	}
	if !d.ArrayElementsAreRefs() || d.ElemSize == 0 {
		return 0, 0
	}
	length := c.header(r).Aux
	first := start / d.ElemSize
	if start%d.ElemSize != 0 {
		first++
	}
	last := end / d.ElemSize
	if first >= length {
		return 0, 0
	}
	if last >= length {
		last = length - 1
	}
	cardEnd := (first*d.ElemSize/c.cardBytes+1)*c.cardBytes - 1
	cardUseful := false
	for i := first; i <= last; i++ {
		off := i * d.ElemSize
		if off > cardEnd {
			if cardUseful {
				usefulCards++
			}
			cardUseful = false
			cardEnd += c.cardBytes
		}
		slots++
		child := Ref(binary.LittleEndian.Uint32(b[PayloadOffset+off:]))
		if c.isNurseryRef(child) {
			cardUseful = true
		}
		c.markNurseryRef(child)
	}
	if cardUseful {
		usefulCards++
	}
	return slots, usefulCards
}
