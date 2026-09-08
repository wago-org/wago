package gc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func (c *Collector) Verify(roots RootSet) error {
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if c.cfg.Profile == ProfileThroughput && c.throughput.limit != 0 {
		if err := c.throughput.verify(c.handles); err != nil {
			return err
		}
	}
	if c.cfg.Profile == ProfileTiny {
		if err := c.verifyTiny(roots); err != nil {
			return err
		}
	}
	for h := uint32(1); int(h) < len(c.handles); h++ {
		e := c.handles[h]
		if e.space == spaceFree {
			continue
		}
		if c.cfg.Profile == ProfileTiny && (c.tinyGC.state == tinySweep || c.tinyGC.rootPhase == tinyRootsSweepBarrier) && e.space == spaceTiny && c.tinyColorOf(h) == tinyWhite {
			// During incremental Tiny sweep, white objects are already unreachable
			// garbage even if their handles have not been reclaimed yet. Earlier
			// sweep steps may have freed other white objects they still reference.
			continue
		}
		r := makeObjRef(h)
		hdr := c.header(r)
		d, err := c.desc(TypeID(hdr.TypeID))
		if err != nil {
			return err
		}
		if hdr.Size != e.size {
			return fmt.Errorf("gc: handle %d size mismatch", h)
		}
		if d.Kind == KindStruct {
			sz, _ := StructSize(d)
			if sz != hdr.Size {
				return fmt.Errorf("gc: struct %d size mismatch", h)
			}
		} else {
			sz, err := ArraySize(d, hdr.Aux)
			if err != nil || sz != hdr.Size {
				return fmt.Errorf("gc: array %d size mismatch", h)
			}
		}
		if d.PointerFree() && d.HasRefs {
			return fmt.Errorf("gc: pointer-free contradiction")
		}
		if err := c.verifyEdges(r, d); err != nil {
			return err
		}
	}
	var rootErr error
	checkRoot := func(r Ref) bool {
		if !validRootRef(c, r) {
			rootErr = errors.New("gc: invalid root ref")
			return false
		}
		return true
	}
	if roots != nil && !rangeRootRefs(roots, checkRoot) {
		roots.RangeRoots(func(slot RootSlot) bool { return checkRoot(slot.GetRef()) })
	}
	if rootErr != nil {
		return rootErr
	}
	for _, r := range c.globalSlots {
		if !validRootRef(c, r) {
			return errors.New("gc: invalid global ref")
		}
	}
	for _, r := range c.tableSlots {
		if !validRootRef(c, r) {
			return errors.New("gc: invalid table ref")
		}
	}
	if err := c.verifyRememberedShadow(); err != nil {
		return err
	}
	if err := c.verifyCardMetadata(); err != nil {
		return err
	}
	return nil
}

// verifyRememberedShadow independently reconstructs the old-to-nursery parent
// set from heap contents. Remembered metadata may conservatively retain parents
// whose young edges were overwritten, so verification requires completeness
// rather than exact equality between collections.
func (c *Collector) verifyRememberedShadow() error {
	seenRemembered := make([]bool, len(c.handles))
	for _, h := range c.remembered {
		if h == 0 || !slotIndexOK(h, len(c.handles)) || c.handles[h].space == spaceFree || !c.handles[h].remembered {
			return fmt.Errorf("gc: invalid remembered handle %d", h)
		}
		if seenRemembered[h] {
			return fmt.Errorf("gc: duplicate remembered handle %d", h)
		}
		seenRemembered[h] = true
	}
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].remembered != seenRemembered[h] {
			return fmt.Errorf("gc: handle %d remembered bit/list mismatch", h)
		}
		if c.cfg.Profile != ProfileThroughput {
			continue
		}
		sp := c.handles[h].space
		if c.handles[h].young() || (sp != spaceOld && sp != spaceLarge) {
			continue
		}
		containsNursery := false
		if err := c.verifyScanObjectRefs(h, func(child Ref) {
			if !containsNursery && c.isNurseryRef(child) {
				containsNursery = true
			}
		}); err != nil {
			return err
		}
		if containsNursery && !seenRemembered[h] {
			return fmt.Errorf("gc: shadow verifier found unremembered old-to-nursery edge from handle %d", h)
		}
		if containsNursery {
			if err := c.verifyNurseryEdgesCarded(h); err != nil {
				return err
			}
		}
	}
	for i, r := range c.globalSlots {
		if c.isNurseryRef(r) && !c.cardFallback && !cardBitIsSet(c.globalCardBits, uint32(i)) {
			return fmt.Errorf("gc: shadow verifier found uncarded nursery global %d", i)
		}
	}
	for i, r := range c.tableSlots {
		if c.isNurseryRef(r) && !c.cardFallback && !cardBitIsSet(c.tableCardBits, uint32(i)) {
			return fmt.Errorf("gc: shadow verifier found uncarded nursery table %d", i)
		}
	}
	return nil
}

// verifyScanObjectRefs intentionally does not call scanObjectRefs. Keeping the
// slow verifier's descriptor walk separate prevents one bad offset or array
// bound implementation from agreeing with itself.
func (c *Collector) verifyScanObjectRefs(h uint32, visit func(Ref)) error {
	if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
		return fmt.Errorf("gc: shadow scan invalid handle %d", h)
	}
	r := makeObjRef(h)
	b := c.bytes(r)
	if len(b) < int(HeaderSize) {
		return fmt.Errorf("gc: shadow scan short object %d", h)
	}
	typeID := TypeID(binary.LittleEndian.Uint32(b[:4]))
	d, err := c.desc(typeID)
	if err != nil {
		return err
	}
	if d.Kind == KindStruct {
		for _, field := range d.Fields {
			if !isCollectorRefKind(field.Kind) {
				continue
			}
			off := uint64(PayloadOffset) + uint64(field.Offset)
			if off+4 > uint64(len(b)) {
				return fmt.Errorf("gc: shadow scan struct field outside handle %d", h)
			}
			visit(Ref(binary.LittleEndian.Uint32(b[off : off+4])))
		}
		return nil
	}
	if d.Kind != KindArray || !isCollectorRefKind(d.Elem) {
		return nil
	}
	length := binary.LittleEndian.Uint32(b[8:12])
	for i := uint32(0); i < length; i++ {
		off := uint64(PayloadOffset) + uint64(i)*uint64(d.ElemSize)
		if off+4 > uint64(len(b)) {
			return fmt.Errorf("gc: shadow scan array element outside handle %d", h)
		}
		visit(Ref(binary.LittleEndian.Uint32(b[off : off+4])))
	}
	return nil
}

func (c *Collector) objectCardCovers(h, payloadOffset uint32) bool {
	if h == 0 || int(h) >= len(c.handles) {
		return false
	}
	for slot, steps := c.handles[h].cardSlot, 0; slot != 0 && steps <= len(c.objectCards); steps++ {
		if !slotIndexOK(slot-1, len(c.objectCards)) {
			return false
		}
		card := c.objectCards[slot-1]
		if card.handle == h && payloadOffset >= card.index && payloadOffset <= card.end {
			return true
		}
		slot = card.next
	}
	return false
}

func (c *Collector) verifyNurseryEdgesCarded(h uint32) error {
	r := makeObjRef(h)
	b := c.bytes(r)
	d, err := c.refDesc(r)
	if err != nil {
		return err
	}
	check := func(off uint32) error {
		child := Ref(binary.LittleEndian.Uint32(b[PayloadOffset+off:]))
		slot := c.handles[h].cardSlot
		if c.isNurseryRef(child) && !c.cardFallback && slot != 0 && !c.objectCardCovers(h, off) {
			return fmt.Errorf("gc: shadow verifier found uncarded old-to-nursery edge from handle %d at payload byte %d", h, off)
		}
		return nil
	}
	if d.Kind == KindStruct {
		for _, field := range d.Fields {
			if isCollectorRefKind(field.Kind) {
				if err := check(field.Offset); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if d.ArrayElementsAreRefs() {
		for i := uint32(0); i < c.header(r).Aux; i++ {
			if err := check(i * d.ElemSize); err != nil {
				return err
			}
		}
	}
	return nil
}

// verifyNurseryEvacuated is the expensive assertion for successful throughput
// minor collections. Eden must be empty; every retained small young object must
// reside in the active survivor semispace and every listed handle must be young.
func (c *Collector) verifyNurseryEvacuated() error {
	listed := make([]bool, len(c.handles))
	for _, h := range c.nurseryHandles {
		if h == 0 || int(h) >= len(c.handles) || listed[h] || !c.handles[h].young() {
			return fmt.Errorf("gc: invalid retained young handle %d", h)
		}
		listed[h] = true
	}
	for h := uint32(1); int(h) < len(c.handles); h++ {
		e := c.handles[h]
		if !e.young() {
			continue
		}
		if !listed[h] {
			return fmt.Errorf("gc: young handle %d missing from dense set", h)
		}
		if e.space == spaceNursery && !c.inActiveSurvivor(e) {
			return fmt.Errorf("gc: handle %d remained outside active survivor space", h)
		}
	}
	if c.nurseryBump != 0 {
		return fmt.Errorf("gc: evacuated Eden retained bump=%d", c.nurseryBump)
	}
	return nil
}

// verifyCardMetadata proves that every card range is reachable from exactly one
// live old/large handle and that the dense persistent-root vectors agree with
// their stable-index bitmaps.
func (c *Collector) verifyCardMetadata() error {
	seenObjectCards := make([]bool, len(c.objectCards))
	freeObjectCards := make([]bool, len(c.objectCards))
	for slot, steps := c.freeObjectCardSlot, 0; slot != 0; steps++ {
		if steps >= len(c.objectCards) || !slotIndexOK(slot-1, len(c.objectCards)) {
			return fmt.Errorf("gc: cyclic or stale free object card slot %d", slot)
		}
		pos := slot - 1
		if freeObjectCards[pos] {
			return fmt.Errorf("gc: free object card slot %d is multiply linked", slot)
		}
		card := c.objectCards[pos]
		if card.handle != 0 || card.index != 0 || card.end != 0 {
			return fmt.Errorf("gc: free object card slot %d retains live metadata", slot)
		}
		freeObjectCards[pos] = true
		slot = card.next
	}
	for h := uint32(1); int(h) < len(c.handles); h++ {
		slot := c.handles[h].cardSlot
		for steps := 0; slot != 0; steps++ {
			if steps > len(c.objectCards) || !slotIndexOK(slot-1, len(c.objectCards)) {
				return fmt.Errorf("gc: handle %d has cyclic or stale object card slot %d", h, slot)
			}
			pos := slot - 1
			if freeObjectCards[pos] {
				return fmt.Errorf("gc: object card slot %d is both live and free", slot)
			}
			if seenObjectCards[pos] {
				return fmt.Errorf("gc: object card slot %d is multiply linked", slot)
			}
			seenObjectCards[pos] = true
			card := c.objectCards[pos]
			if card.handle != h || (c.handles[h].space != spaceOld && c.handles[h].space != spaceLarge) {
				return fmt.Errorf("gc: object card slot %d owner=%d, want live old/large handle %d", slot, card.handle, h)
			}
			payloadBytes := c.handles[h].size - PayloadOffset
			if card.end < card.index || card.index >= payloadBytes || card.end >= payloadBytes || card.index%c.cardBytes != 0 {
				return fmt.Errorf("gc: invalid object card range %d..%d for handle %d payload %d", card.index, card.end, h, payloadBytes)
			}
			if card.end != payloadBytes-1 && (card.end+1)%c.cardBytes != 0 {
				return fmt.Errorf("gc: unaligned object card end %d for handle %d", card.end, h)
			}
			slot = card.next
		}
	}
	for i, card := range c.objectCards {
		if card.handle == 0 {
			if !freeObjectCards[i] {
				return fmt.Errorf("gc: tombstoned object card %d is not reusable", i)
			}
			continue
		}
		if !seenObjectCards[i] {
			return fmt.Errorf("gc: unreachable object card slot %d for handle %d", i+1, card.handle)
		}
	}
	seenGlobals := make([]bool, len(c.globalSlots))
	seenTables := make([]bool, len(c.tableSlots))
	for _, card := range c.slotCards {
		var seen []bool
		var bits []uint64
		switch card.kind {
		case SlotGlobal:
			seen, bits = seenGlobals, c.globalCardBits
		case SlotTable:
			seen, bits = seenTables, c.tableCardBits
		default:
			return fmt.Errorf("gc: invalid slot card kind %d", card.kind)
		}
		if !slotIndexOK(card.index, len(seen)) || seen[card.index] || !cardBitIsSet(bits, card.index) {
			return fmt.Errorf("gc: invalid or duplicate slot card %d:%d", card.kind, card.index)
		}
		seen[card.index] = true
	}
	for i := range seenGlobals {
		if cardBitIsSet(c.globalCardBits, uint32(i)) != seenGlobals[i] {
			return fmt.Errorf("gc: global card bit/list mismatch at %d", i)
		}
	}
	for i := range seenTables {
		if cardBitIsSet(c.tableCardBits, uint32(i)) != seenTables[i] {
			return fmt.Errorf("gc: table card bit/list mismatch at %d", i)
		}
	}
	return nil
}

func (c *Collector) verifyEdges(r Ref, d TypeDesc) error {
	check := func(x Ref) error {
		if x.IsNull() || x.IsI31() {
			return nil
		}
		if !x.IsObj() {
			return errors.New("gc: invalid ref encoding")
		}
		h := handleOf(x)
		if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
			return errors.New("gc: live object points to freed object")
		}
		return nil
	}
	b := c.bytes(r)
	hdr := c.header(r)
	if d.Kind == KindStruct {
		for _, f := range d.Fields {
			if isCollectorRefKind(f.Kind) {
				if err := check(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+f.Offset:]))); err != nil {
					return err
				}
			}
		}
	} else if d.ArrayElementsAreRefs() {
		for i := uint32(0); i < hdr.Aux; i++ {
			if err := check(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+i*d.ElemSize:]))); err != nil {
				return err
			}
		}
	}
	return nil
}
func validRootRef(c *Collector, r Ref) bool {
	if r.IsNull() || r.IsI31() {
		return true
	}
	if !r.IsObj() {
		return false
	}
	h := handleOf(r)
	return h != 0 && int(h) < len(c.handles) && c.handles[h].space != spaceFree
}
