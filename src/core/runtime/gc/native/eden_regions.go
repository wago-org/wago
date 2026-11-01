package gc

import "errors"

// A region owns an exclusive interval of Eden. Live objects never span regions.
// Occupancy is established by exact full tracing, never by compiler hints.
type edenRegion struct {
	bump uint32 // relative; only reset once no live handle occupies this region
	live bool   // scratch for full collection
}

func (c *Collector) regionAllocationOffset(size, alignment uint32) (uint32, bool) {
	width := (uint32(1) << c.cfg.NurseryRegionShift)
	count := uint32(len(c.edenRegions))
	for i := uint32(0); i < count; i++ {
		index := (c.edenRegionCursor + i) % count
		start := align(c.edenRegions[index].bump, alignment)
		if start <= width && size <= width-start {
			return index*width + start, true
		}
	}
	return 0, false
}

func (c *Collector) commitRegionAllocation(off, size uint32) {
	width := (uint32(1) << c.cfg.NurseryRegionShift)
	index := off / width
	c.edenRegions[index].bump = off%width + size
	c.edenRegionCursor = index
}

func (c *Collector) reclaimEmptyEdenRegions() {
	for i := range c.edenRegions {
		c.edenRegions[i].live = false
	}
	for _, h := range c.nurseryHandles {
		e := c.handles[h]
		if e.space == spaceNursery && e.off < c.edenBytes() {
			c.edenRegions[e.off/(uint32(1)<<c.cfg.NurseryRegionShift)].live = true
		}
	}
	for i := range c.edenRegions {
		if !c.edenRegions[i].live {
			c.edenRegions[i].bump = 0
		}
	}
	c.edenRegionCursor = 0
}

func (c *Collector) verifyEdenRegions() error {
	if len(c.edenRegions) == 0 {
		return nil
	}
	width := uint32(1) << c.cfg.NurseryRegionShift
	for _, r := range c.edenRegions {
		if r.bump > width {
			return errors.New("gc: Eden region bump out of bounds")
		}
	}
	for _, e := range c.handles {
		if e.space != spaceNursery || e.off >= c.edenBytes() {
			continue
		}
		start := e.off % width
		if e.size > width-start || start+e.size > c.edenRegions[e.off/width].bump {
			return errors.New("gc: live object exceeds Eden region allocation")
		}
	}
	return nil
}
