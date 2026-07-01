package gc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func (c *Collector) CollectFull(roots RootSet) error {
	c.stats.FullCollections++
	if c.cfg.Profile == ProfileTiny {
		if err := c.tinyCollectFull(roots); err != nil {
			return err
		}
		if c.cfg.VerifyAfterCollect {
			return c.Verify(roots)
		}
		return nil
	}
	c.clearMarks()
	c.markRoots(roots)
	c.sweepAll()
	if c.cfg.VerifyAfterCollect {
		return c.Verify(roots)
	}
	return nil
}
func (c *Collector) CollectMinor(roots RootSet) error {
	c.stats.MinorCollections++
	if c.cfg.Profile == ProfileTiny {
		// Tiny is non-generational; minor collection is defined as a complete
		// incremental mark/sweep cycle for API compatibility.
		if err := c.tinyCollectFull(roots); err != nil {
			return err
		}
		if c.cfg.VerifyAfterCollect {
			return c.Verify(roots)
		}
		return nil
	}
	// This first pass implements minor as exact marking of nursery reachability
	// plus promotion of survivors into old space. Remembered old objects and
	// global/table slots are treated as additional roots for young objects.
	c.clearMarks()
	c.markRoots(roots)
	for _, h := range c.remembered {
		if int(h) < len(c.handles) && (c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge) {
			c.scanObject(h)
		}
	}
	c.drainMarkStack()
	if err := c.promoteMarkedNursery(); err != nil {
		return err
	}
	c.sweepNurseryDead()
	if c.cfg.ForceMajorEveryMinor {
		if err := c.CollectFull(roots); err != nil {
			return err
		}
	}
	if c.cfg.VerifyAfterCollect {
		return c.Verify(roots)
	}
	return nil
}
func (c *Collector) clearMarks() {
	if len(c.mark) < len(c.handles) {
		c.mark = make([]bool, len(c.handles))
	}
	for i := range c.mark {
		c.mark[i] = false
	}
	c.markStack = c.markStack[:0]
}
func (c *Collector) markRoots(roots RootSet) {
	if roots != nil {
		roots.RangeRoots(func(s RootSlot) bool { c.markRef(s.GetRef()); return true })
	}
	for _, r := range c.globalSlots {
		c.markRef(r)
	}
	for _, r := range c.tableSlots {
		c.markRef(r)
	}
	c.drainMarkStack()
}
func (c *Collector) drainMarkStack() {
	for len(c.markStack) > 0 {
		n := len(c.markStack) - 1
		h := c.markStack[n]
		c.markStack = c.markStack[:n]
		c.scanObject(h)
	}
}
func (c *Collector) markRef(r Ref) {
	if !r.IsObj() {
		return
	}
	h := handleOf(r)
	if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
		return
	}
	if c.mark[h] {
		return
	}
	c.mark[h] = true
	c.markStack = append(c.markStack, h)
}
func (c *Collector) scanObject(h uint32) { c.scanObjectRefs(h, c.markRef) }
func (c *Collector) sweepAll() {
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space != spaceFree && !c.mark[h] {
			c.free(h)
		}
	}
	c.compactNurseryBump()
}
func (c *Collector) sweepNurseryDead() {
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space == spaceNursery && !c.mark[h] {
			c.free(h)
		}
	}
	c.compactNurseryBump()
}

type plannedPromotion struct {
	handle uint32
	entry  handleEntry
}

func (c *Collector) promoteMarkedNursery() error {
	plans := make([]plannedPromotion, 0)
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space == spaceNursery && c.mark[h] {
			e, err := c.throughput.alloc(c.handles[h].size, spaceOld)
			if err != nil {
				for i := len(plans) - 1; i >= 0; i-- {
					_ = c.throughput.free(plans[i].entry)
				}
				return err
			}
			plans = append(plans, plannedPromotion{handle: h, entry: e})
		}
	}
	for _, p := range plans {
		c.promoteHandleTo(p.handle, p.entry)
	}
	return nil
}
func (c *Collector) promoteHandle(h uint32) error {
	if int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
		return errors.New("gc: invalid handle")
	}
	if c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge {
		return nil
	}
	oldEntry, err := c.throughput.alloc(c.handles[h].size, spaceOld)
	if err != nil {
		return err
	}
	c.promoteHandleTo(h, oldEntry)
	return nil
}
func (c *Collector) promoteHandleTo(h uint32, oldEntry handleEntry) {
	e := &c.handles[h]
	src := c.nursery[e.off : e.off+e.size]
	dst := c.throughput.bytes(oldEntry)
	copy(dst, src)
	flags := binary.LittleEndian.Uint32(dst[12:16]) | FlagOld
	binary.LittleEndian.PutUint32(dst[12:16], flags)
	if c.cfg.PoisonFreed {
		for i := range src {
			src[i] = 0xdd
		}
	}
	*e = oldEntry
}
func (c *Collector) free(h uint32) {
	c.removeRemembered(h)
	c.removeCardsForHandle(h)
	e := &c.handles[h]
	if c.cfg.PoisonFreed {
		switch e.space {
		case spaceNursery:
			for i := range c.nursery[e.off : e.off+e.size] {
				c.nursery[e.off+uint32(i)] = 0xdd
			}
		case spaceTiny:
			bi := e.off / c.tiny.blockBytes
			span := c.tiny.blocks[bi].size * c.tiny.blockBytes
			for i := range c.tiny.mem[e.off : e.off+span] {
				c.tiny.mem[e.off+uint32(i)] = 0xdd
			}
		case spaceOld, spaceLarge:
			end := e.off + e.allocSize
			if end > uint32(len(c.throughput.mem)) {
				end = uint32(len(c.throughput.mem))
			}
			for i := range c.throughput.mem[e.off:end] {
				c.throughput.mem[e.off+uint32(i)] = 0xdd
			}
		}
	}
	if e.space == spaceTiny {
		_ = c.tiny.free(e.off)
		c.tinySetColor(h, tinyWhite)
	} else if e.space == spaceOld || e.space == spaceLarge {
		_ = c.throughput.free(*e)
	}
	*e = handleEntry{}
	c.freeHandles = append(c.freeHandles, h)
}
func (c *Collector) compactNurseryBump() {
	var max uint32
	for _, e := range c.handles {
		if e.space == spaceNursery && e.off+e.size > max {
			max = e.off + e.size
		}
	}
	c.nurseryBump = max
}
func (c *Collector) liveCount() uint32 {
	var n uint32
	for _, e := range c.handles {
		if e.space != spaceFree {
			n++
		}
	}
	return n
}

func (c *Collector) Verify(roots RootSet) error {
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
