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
		if c.cfg.Profile == ProfileTiny && c.tinyGC.state == tinySweep && e.space == spaceTiny && c.tinyColorOf(h) == tinyWhite {
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
	}
	if err := c.verifyCardMetadata(); err != nil {
		return err
	}
	return nil
}

// verifyCardMetadata checks only metadata ownership/bounds. Card entries are a
// scaffold for future card scanning, so they may outlive the exact young edge
// that created them; Verify rejects stale object handles and unsupported or
// out-of-range slot cards before later collectors can depend on them.
func (c *Collector) verifyCardMetadata() error {
	for i, card := range c.objectCards {
		if card.handle == 0 || !slotIndexOK(card.handle, len(c.handles)) || c.handles[card.handle].space == spaceFree {
			return fmt.Errorf("gc: invalid object card handle %d", card.handle)
		}
		if card.end < card.index {
			return fmt.Errorf("gc: invalid object card range %d..%d", card.index, card.end)
		}
		if c.handles[card.handle].cardSlot != uint32(i+1) {
			return fmt.Errorf("gc: object card handle %d slot=%d, want %d", card.handle, c.handles[card.handle].cardSlot, i+1)
		}
	}
	for h := uint32(1); int(h) < len(c.handles); h++ {
		slot := c.handles[h].cardSlot
		if slot == 0 {
			continue
		}
		if !slotIndexOK(slot-1, len(c.objectCards)) || c.objectCards[slot-1].handle != h {
			return fmt.Errorf("gc: handle %d has stale object card slot %d", h, slot)
		}
	}
	if len(c.slotCardSlot) != len(c.slotCards) {
		return fmt.Errorf("gc: slot card index size %d, want %d", len(c.slotCardSlot), len(c.slotCards))
	}
	for i, card := range c.slotCards {
		switch card.kind {
		case SlotGlobal:
			if !slotIndexOK(card.index, len(c.globalSlots)) {
				return fmt.Errorf("gc: invalid global slot card %d", card.index)
			}
		case SlotTable:
			if !slotIndexOK(card.index, len(c.tableSlots)) {
				return fmt.Errorf("gc: invalid table slot card %d", card.index)
			}
		default:
			return fmt.Errorf("gc: invalid slot card kind %d", card.kind)
		}
		if slot := c.slotCardSlot[slotCardKey(card.kind, card.index)]; slot != uint32(i+1) {
			return fmt.Errorf("gc: slot card %d:%d slot=%d, want %d", card.kind, card.index, slot, i+1)
		}
	}
	for key, slot := range c.slotCardSlot {
		if slot == 0 || !slotIndexOK(slot-1, len(c.slotCards)) {
			return fmt.Errorf("gc: stale slot card index %#x=%d", key, slot)
		}
		card := c.slotCards[slot-1]
		if slotCardKey(card.kind, card.index) != key {
			return fmt.Errorf("gc: slot card index %#x points to %d:%d", key, card.kind, card.index)
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
