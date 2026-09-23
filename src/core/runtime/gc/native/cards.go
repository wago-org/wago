package gc

import (
	"encoding/binary"
	"time"
)

// scanRememberedCards traces the exact dirty payload cards for one old/large
// object only when the complete linked chain is valid. Missing, degraded, stale,
// cyclic, or otherwise malformed metadata is never authoritative: the object is
// scanned completely and the collector-wide fallback remains set while young
// objects survive. Duplicate scanning after a valid prefix is safe; omitting a
// reference outside that prefix is not.
func (c *Collector) scanRememberedCards(h uint32) {
	if h == 0 || int(h) >= len(c.handles) {
		return
	}
	e := &c.handles[h]
	if e.young() || (e.space != spaceOld && e.space != spaceLarge) {
		return
	}
	if c.cardFallback || e.cardSlot == 0 {
		// Metadata growth failure is fail-safe: remembered membership remains
		// authoritative and falls back to complete object scans until evacuation
		// clears the young generation.
		c.scanObjectRefs(h, c.markNurseryRef)
		return
	}
	if len(c.objectCards) >= 16 && c.hasSixteenObjectCardRanges(e.cardSlot) && c.scanStructCardRanges(h) {
		return
	}
	startTime := time.Time{}
	if c.telemetryEnabled() {
		startTime = c.cfg.Telemetry.scanStart()
	}
	payloadSize := uint32(0)
	if e.size > PayloadOffset {
		payloadSize = e.size - PayloadOffset
	}
	valid := payloadSize != 0 && c.cardBytes != 0
	var payloadBytes, slots, dirtyCards, usefulCards uint64
	for slot, steps := e.cardSlot, 0; valid && slot != 0; steps++ {
		if steps >= len(c.objectCards) || !slotIndexOK(slot-1, len(c.objectCards)) {
			valid = false
			break
		}
		card := c.objectCards[slot-1]
		if card.handle != h || card.end < card.index || card.index >= payloadSize || card.end >= payloadSize ||
			card.index%c.cardBytes != 0 || (card.end != payloadSize-1 && (card.end+1)%c.cardBytes != 0) {
			valid = false
			break
		}
		n, useful := c.scanObjectPayloadRange(h, card.index, card.end)
		slots += uint64(n)
		payloadBytes += uint64(card.end-card.index) + 1
		dirtyCards += uint64(card.end/c.cardBytes-card.index/c.cardBytes) + 1
		usefulCards += uint64(useful)
		slot = card.next
	}
	if !valid {
		// Detach the untrusted chain from its authoritative handle before falling
		// back. The backing records remain intact for strict Verify diagnostics,
		// but later writes and complete metadata clearing cannot follow a stale or
		// wrong-owner link.
		c.lastCardBounds = objectCardBounds{}
		e.cardSlot = 0
		c.cardFallback = true
		c.scanObjectRefs(h, c.markNurseryRef)
		return
	}
	if c.telemetryEnabled() {
		whole := payloadBytes >= uint64(payloadSize)
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
		span := end - start
		lastCard := ^uint32(0)
		cardUseful := false
		for _, field := range d.Fields {
			// Unsigned subtraction also excludes offsets below start.
			if field.Offset-start > span || !isCollectorRefKind(field.Kind) {
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

// hasSixteenObjectCardRanges checks only the bounded admission count. Full
// ownership, range and chain validation remains in the scanners. A short or
// invalid prefix uses the original scanner without entering the large frame.
func (c *Collector) hasSixteenObjectCardRanges(slot uint32) bool {
	for i := 0; i < 15; i++ {
		if slot == 0 || !slotIndexOK(slot-1, len(c.objectCards)) {
			return false
		}
		slot = c.objectCards[slot-1].next
	}
	return slot != 0 && slotIndexOK(slot-1, len(c.objectCards))
}

// structCardRange keeps the original per-range descriptor-order telemetry state.
// The fixed bound limits stack use and sorting work; no descriptor is copied.
type structCardRange struct {
	start, end, lastCard uint32
	useful               bool
}

// scanStructCardRanges handles wide structs with16 to32 disjoint ranges.
// It supports any descriptor field order. Failed admission has no marking or
// metadata side effects, so the general scanner retains all fallback behavior.
func (c *Collector) scanStructCardRanges(h uint32) bool {
	d, err := c.refDesc(makeObjRef(h))
	if err != nil || d.Kind != KindStruct || !d.HasRefs || len(d.Fields) < 256 {
		return false
	}
	startTime := time.Time{}
	if c.telemetryEnabled() {
		startTime = c.cfg.Telemetry.scanStart()
	}
	e := &c.handles[h]
	if e.size <= PayloadOffset || c.cardBytes == 0 {
		return false
	}
	payloadSize := e.size - PayloadOffset
	var ranges [32]structCardRange
	count := 0
	var payloadBytes, dirtyCards uint64
	for slot := e.cardSlot; slot != 0; {
		if count == len(ranges) || !slotIndexOK(slot-1, len(c.objectCards)) {
			return false
		}
		card := c.objectCards[slot-1]
		if card.handle != h || card.end < card.index || card.end >= payloadSize || card.index%c.cardBytes != 0 || (card.end != payloadSize-1 && (card.end+1)%c.cardBytes != 0) {
			return false
		}
		ranges[count] = structCardRange{start: card.index, end: card.end, lastCard: ^uint32(0)}
		count++
		payloadBytes += uint64(card.end-card.index) + 1
		dirtyCards += uint64(card.end/c.cardBytes-card.index/c.cardBytes) + 1
		slot = card.next
	}
	if count < 16 {
		return false
	}
	// Insertion sort is bounded to32 ranges and uses no closure or allocation.
	for i := 1; i < count; i++ {
		value := ranges[i]
		j := i
		for j > 0 && ranges[j-1].start > value.start {
			ranges[j] = ranges[j-1]
			j--
		}
		ranges[j] = value
	}
	for i := 1; i < count; i++ {
		if ranges[i-1].end >= ranges[i].start {
			return false
		}
	}
	b := c.bytes(makeObjRef(h))
	var slots, usefulCards uint64
	for _, field := range d.Fields {
		if !isCollectorRefKind(field.Kind) {
			continue
		}
		lo, hi := 0, count
		for lo < hi {
			mid := lo + (hi-lo)/2
			if ranges[mid].end < field.Offset {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo == count || field.Offset < ranges[lo].start {
			continue
		}
		state := &ranges[lo]
		card := field.Offset / c.cardBytes
		if card != state.lastCard {
			if state.useful {
				usefulCards++
			}
			state.lastCard, state.useful = card, false
		}
		slots++
		child := Ref(binary.LittleEndian.Uint32(b[PayloadOffset+field.Offset:]))
		if c.isNurseryRef(child) {
			state.useful = true
		}
		c.markNurseryRef(child)
	}
	for i := 0; i < count; i++ {
		if ranges[i].useful {
			usefulCards++
		}
	}
	if c.telemetryEnabled() {
		c.cfg.Telemetry.noteCardScan(startTime, payloadBytes, slots, dirtyCards, usefulCards, payloadBytes >= uint64(payloadSize))
	}
	return true
}
