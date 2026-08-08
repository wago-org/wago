package gc

import (
	"cmp"
	"encoding/binary"
	"errors"
	"slices"
)

func (c *Collector) CollectFull(roots RootSet) error {
	if err := c.errIfClosed(); err != nil {
		return err
	}
	c.discardNativeStructHandles()
	defer c.refreshNativeView()
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
	c.discardNativeStructHandles()
	defer c.refreshNativeView()
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
	// Minor collection traces nursery reachability only. Old/large roots are not
	// recursively scanned; every direct old/large-to-nursery edge is represented
	// by the remembered set, while global/table slots are scanned as roots.
	c.clearNurseryMarks()
	c.markNurseryRoots(roots)
	if c.cfg.VerifyAfterCollect {
		if err := c.verifyRememberedShadow(); err != nil {
			return err
		}
	}
	for _, h := range c.remembered {
		if int(h) < len(c.handles) && (c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge) {
			c.stats.MinorRememberedScanned++
			c.scanObjectRefs(h, c.markNurseryRef)
		}
	}
	if survivors := c.drainNurseryMarkStack(); survivors != 0 {
		if err := c.promoteMarkedNursery(); err != nil {
			c.clearNurseryMarks()
			return err
		}
	}
	c.finishMinorEvacuation()
	c.clearCardMetadata() // cards are verification scaffolding, not collection inputs
	if c.cfg.VerifyAfterCollect {
		if err := c.verifyNurseryEvacuated(); err != nil {
			return err
		}
	}
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
			if c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge {
				c.deferThroughputFree(h)
			} else {
				c.free(h)
			}
		}
	}
	c.compactNurseryHandles()
	c.compactNurseryBump()
}

// finishMinorEvacuation commits the destructive half of a successful minor
// collection. promoteMarkedNursery has moved every live nursery object before
// this is called, so every handle still pointing into the nursery is dead.
func (c *Collector) finishMinorEvacuation() {
	for _, h := range c.nurseryHandles {
		if h == 0 || int(h) >= len(c.handles) {
			continue
		}
		c.mark[h] = false
		if c.handles[h].space == spaceNursery {
			c.free(h)
		}
	}
	clear(c.nurseryHandles)
	c.nurseryHandles = c.nurseryHandles[:0]
	c.nurseryBump = 0
	c.clearRememberedMetadata()
}

type plannedPromotion struct {
	handle uint32
	entry  handleEntry
}

func (c *Collector) promoteMarkedNursery() error {
	plans := c.promotionScratch[:0]
	for _, h := range c.nurseryHandles {
		if h != 0 && int(h) < len(c.handles) && c.handles[h].space == spaceNursery && c.mark[h] {
			if err := injectFailure(c, failPromotionPlan); err != nil {
				clear(plans)
				c.promotionScratch = plans[:0]
				return err
			}
			plans = append(plans, plannedPromotion{handle: h})
		}
	}
	// Group destinations by allocation size. Equal-size survivors retain handle
	// order, while old-space reuse and bump destinations stay clustered by size
	// class/page instead of following arbitrary nursery graph order.
	if len(plans) > 1 {
		slices.SortStableFunc(plans, func(a, b plannedPromotion) int {
			return cmp.Compare(c.throughput.promotionAllocSize(c.handles[a.handle].size), c.throughput.promotionAllocSize(c.handles[b.handle].size))
		})
	}

	if err := c.throughput.sweepAllPending(); err != nil {
		clear(plans)
		c.promotionScratch = plans[:0]
		return err
	}
	tx := c.throughput.beginAllocTransaction()
	allocated := 0
	finish := func() {
		clear(plans)
		c.promotionScratch = plans[:0]
	}
	rollback := func(current *handleEntry) {
		if current != nil {
			c.throughput.rollbackSuccessfulAlloc(*current, tx.bump)
		}
		for i := allocated - 1; i >= 0; i-- {
			c.throughput.rollbackSuccessfulAlloc(plans[i].entry, tx.bump)
		}
		c.throughput.restoreAllocTransaction(tx)
		finish()
	}
	for i := range plans {
		e, err := c.throughput.alloc(c.handles[plans[i].handle].size, spaceOld)
		if err != nil {
			rollback(nil)
			return err
		}
		if err := injectFailure(c, failPromotionDestination); err != nil {
			rollback(&e)
			return err
		}
		plans[i].entry = e
		allocated++
	}
	for range plans {
		if err := injectFailure(c, failPromotionCommit); err != nil {
			rollback(nil)
			return err
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
	if err := c.throughput.sweepAllPending(); err != nil {
		return err
	}
	tx := c.throughput.beginAllocTransaction()
	oldEntry, err := c.throughput.alloc(c.handles[h].size, spaceOld)
	if err != nil {
		c.throughput.restoreAllocTransaction(tx)
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
func (c *Collector) free(h uint32) { c.releaseHandle(h, false) }

func (c *Collector) deferThroughputFree(h uint32) { c.releaseHandle(h, true) }

func (c *Collector) releaseHandle(h uint32, lazyThroughput bool) {
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
			span := c.tiny.spanSize(bi) * c.tiny.blockBytes
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
		if lazyThroughput {
			_ = c.throughput.deferFree(*e)
		} else {
			_ = c.throughput.free(*e)
		}
	}
	*e = handleEntry{}
	c.freeHandles = append(c.freeHandles, h)
}
func (c *Collector) compactNurseryHandles() {
	out := c.nurseryHandles[:0]
	for _, h := range c.nurseryHandles {
		if h != 0 && int(h) < len(c.handles) && c.handles[h].space == spaceNursery {
			out = append(out, h)
		}
	}
	clear(c.nurseryHandles[len(out):])
	c.nurseryHandles = out
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
