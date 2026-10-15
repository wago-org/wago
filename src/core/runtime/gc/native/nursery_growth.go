package gc

// growDefaultNursery trades bounded backing for fewer premature promotions.
// It runs only after successful evacuation and card cleanup. Live survivor bytes
// move as one bounded span; compact references retain their handle identity.
// Eight low-survival collections clear an isolated pressure event.
func (c *Collector) growDefaultNursery(copiedBytes uint64) {
	eden := c.cfg.NurseryBytes
	if c.nurseryGrowthLimit == 0 || eden >= c.nurseryGrowthLimit {
		return
	}
	if copiedBytes < uint64(eden)/4 {
		if c.nurseryGrowthPressure != 0 {
			c.nurseryGrowthQuiet++
			if c.nurseryGrowthQuiet == 8 {
				c.nurseryGrowthPressure = 0
				c.nurseryGrowthQuiet = 0
			}
		}
		return
	}
	c.nurseryGrowthQuiet = 0
	if c.nurseryGrowthPressure < 2 {
		c.nurseryGrowthPressure++
	}
	if c.nurseryGrowthPressure < 2 || c.nurseryBump != 0 || c.survivorBump > c.survivorBytes {
		return
	}
	oldBase := c.survivorBase(c.survivorFrom)
	for _, h := range c.nurseryHandles {
		if h == 0 || int(h) >= len(c.handles) {
			return
		}
		e := c.handles[h]
		if e.space == spaceNursery && (e.off < oldBase || e.off-oldBase > c.survivorBump || e.size > c.survivorBump-(e.off-oldBase)) {
			return
		}
	}
	eden = min(eden*2, c.nurseryGrowthLimit)
	survivor := eden / 2
	backing := makeAlignedBytes(eden+2*survivor, uintptr(c.objectAlign))
	newBase := eden + uint32(c.survivorFrom&1)*survivor
	copy(backing[newBase:newBase+c.survivorBump], c.nursery[oldBase:oldBase+c.survivorBump])
	for _, h := range c.nurseryHandles {
		if e := &c.handles[h]; e.space == spaceNursery {
			e.off = newBase + (e.off - oldBase)
		}
	}
	c.stats.YoungBytesCopied += uint64(c.survivorBump)
	c.lastCardBounds = objectCardBounds{}
	c.nursery = backing
	c.cfg.NurseryBytes = eden
	c.cfg.SurvivorBytes = survivor
	c.survivorBytes = survivor
	// The larger survivor space must get a chance to retain objects before the
	// occupancy policy can lower the threshold again.
	c.tenuringThreshold = defaultTenuringThreshold
	c.nurseryGrowthPressure = 0
	c.nurseryGrowthQuiet = 0
}
