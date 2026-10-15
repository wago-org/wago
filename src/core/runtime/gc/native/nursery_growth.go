package gc

// growDefaultNursery trades bounded backing for fewer premature promotions.
// It runs only after successful evacuation and card cleanup. Waiting until no
// nursery object remains avoids relocating a live object or changing a handle.
// Eight low-survival collections clear an isolated pressure event.
func (c *Collector) growDefaultNursery(copiedBytes uint64) {
	eden := c.cfg.NurseryBytes
	if c.nurseryGrowthLimit == 0 || eden >= c.nurseryGrowthLimit {
		return
	}
	if copiedBytes < uint64(eden)/2 {
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
	if c.nurseryGrowthPressure < 2 || c.nurseryBump != 0 || c.survivorBump != 0 {
		return
	}
	for _, h := range c.nurseryHandles {
		if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceNursery {
			return
		}
	}
	eden = min(eden*2, c.nurseryGrowthLimit)
	survivor := eden / 2
	backing := makeAlignedBytes(eden+2*survivor, uintptr(c.objectAlign))
	c.nursery = backing
	c.cfg.NurseryBytes = eden
	c.cfg.SurvivorBytes = survivor
	c.survivorBytes = survivor
	c.survivorFrom = 0
	// The larger survivor space must get a chance to retain objects before the
	// occupancy policy can lower the threshold again.
	c.tenuringThreshold = defaultTenuringThreshold
	c.nurseryGrowthPressure = 0
	c.nurseryGrowthQuiet = 0
}
