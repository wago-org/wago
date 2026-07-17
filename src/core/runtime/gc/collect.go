package gc

import (
	"encoding/binary"
	"errors"
)

func (c *Collector) CollectFull(roots RootSet) error {
	if err := c.errIfClosed(); err != nil {
		return err
	}
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
	c.pruneRemembered()
	c.clearCardMetadata() // cards are verification scaffolding, not collection inputs
	if c.cfg.VerifyAfterCollect {
		return c.Verify(roots)
	}
	return nil
}
func (c *Collector) CollectMinor(roots RootSet) error {
	if err := c.errIfClosed(); err != nil {
		return err
	}
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
	c.pruneRemembered()
	c.clearCardMetadata() // cards are verification scaffolding, not collection inputs
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
	plans := c.promotionScratch[:0]
	finish := func() {
		clear(plans)
		c.promotionScratch = plans[:0]
	}
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space == spaceNursery && c.mark[h] {
			e, err := c.throughput.alloc(c.handles[h].size, spaceOld)
			if err != nil {
				for i := len(plans) - 1; i >= 0; i-- {
					_ = c.throughput.free(plans[i].entry)
				}
				finish()
				return err
			}
			plans = append(plans, plannedPromotion{handle: h, entry: e})
		}
	}
	for _, p := range plans {
		c.promoteHandleTo(p.handle, p.entry)
	}
	finish()
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
