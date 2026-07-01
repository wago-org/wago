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
	if roots != nil {
		var err error
		roots.RangeRoots(func(s RootSlot) bool {
			if !validRootRef(c, s.GetRef()) {
				err = errors.New("gc: invalid root ref")
				return false
			}
			return true
		})
		if err != nil {
			return err
		}
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
	for _, h := range c.remembered {
		if h == 0 || !slotIndexOK(h, len(c.handles)) || c.handles[h].space == spaceFree {
			return fmt.Errorf("gc: invalid remembered handle %d", h)
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
	for _, card := range c.objectCards {
		if card.handle == 0 || !slotIndexOK(card.handle, len(c.handles)) || c.handles[card.handle].space == spaceFree {
			return fmt.Errorf("gc: invalid object card handle %d", card.handle)
		}
	}
	for _, card := range c.slotCards {
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
			if isRefKind(f.Kind) {
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
