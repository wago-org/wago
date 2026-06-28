package gc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func (c *Collector) CollectFull(roots RootSet) error {
	c.stats.FullCollections++
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
	c.promoteMarkedNursery()
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
func (c *Collector) scanObject(h uint32) {
	r := makeObjRef(h)
	d, err := c.refDesc(r)
	if err != nil || !d.HasRefs {
		return
	}
	hdr := c.header(r)
	b := c.bytes(r)
	if d.Kind == KindStruct {
		for _, f := range d.Fields {
			if isRefKind(f.Kind) {
				c.markRef(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+f.Offset:])))
			}
		}
	} else if d.ArrayElementsAreRefs() {
		for i := uint32(0); i < hdr.Aux; i++ {
			off := PayloadOffset + i*d.ElemSize
			c.markRef(Ref(binary.LittleEndian.Uint32(b[off:])))
		}
	}
}
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
func (c *Collector) promoteMarkedNursery() {
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space == spaceNursery && c.mark[h] {
			_ = c.promoteHandle(h)
		}
	}
}
func (c *Collector) promoteHandle(h uint32) error {
	if int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
		return errors.New("gc: invalid handle")
	}
	if c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge {
		return nil
	}
	e := &c.handles[h]
	oldOff := uint32(len(c.old))
	obj := append([]byte(nil), c.nursery[e.off:e.off+e.size]...)
	hdr := ObjHeader{binary.LittleEndian.Uint32(obj[0:4]), binary.LittleEndian.Uint32(obj[4:8]), binary.LittleEndian.Uint32(obj[8:12]), binary.LittleEndian.Uint32(obj[12:16])}
	hdr.Flags |= FlagOld
	binary.LittleEndian.PutUint32(obj[12:16], hdr.Flags)
	c.old = append(c.old, obj...)
	if c.cfg.PoisonFreed {
		for i := range c.nursery[e.off : e.off+e.size] {
			c.nursery[e.off+uint32(i)] = 0xdd
		}
	}
	e.off = oldOff
	e.space = spaceOld
	return nil
}
func (c *Collector) free(h uint32) {
	c.removeRemembered(h)
	c.removeCardsForHandle(h)
	e := &c.handles[h]
	if c.cfg.PoisonFreed && e.space == spaceNursery {
		for i := range c.nursery[e.off : e.off+e.size] {
			c.nursery[e.off+uint32(i)] = 0xdd
		}
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
	for _, h := range c.remembered {
		if int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
			return fmt.Errorf("gc: invalid remembered handle %d", h)
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
