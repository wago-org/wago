package gc

const (
	denseNurseryChargeBytes  = 40
	denseNurseryPregrowBytes = 3 << 19
)

// growDefaultNursery trades bounded backing for fewer premature promotions.
// It runs only after successful evacuation and card cleanup. As in V8, survived
// bytes accumulate across collections while Eden is small. Copies below one
// eighth of Eden count as quiet, which keeps sparse survivors from permanently
// growing a nursery that cannot shrink. Above 1 MiB, two quarter-Eden survival
// collections are required. Eight quiet collections clear accumulated pressure.
// Dense small-object streams pre-grow once to avoid retaining each intermediate
// backing while still leaving wider object streams on ordinary doubling.
func (c *Collector) growDefaultNursery(copiedBytes uint64, youngObjects uint32) {
	eden := c.cfg.NurseryBytes
	if c.nurseryGrowthLimit == 0 || eden >= c.nurseryGrowthLimit {
		return
	}
	if eden < 1<<20 {
		if copiedBytes < uint64(eden)/8 {
			if c.nurseryGrowthSurvived != 0 {
				c.nurseryGrowthQuiet++
				if c.nurseryGrowthQuiet == 8 {
					c.nurseryGrowthSurvived = 0
					c.nurseryGrowthQuiet = 0
				}
			}
			return
		}
		c.nurseryGrowthQuiet = 0
		survived := uint64(c.nurseryGrowthSurvived) + copiedBytes
		if survived <= uint64(eden) {
			c.nurseryGrowthSurvived = uint32(survived)
			return
		}
		c.nurseryGrowthSurvived = eden
	} else if copiedBytes < uint64(eden)/4 {
		if c.nurseryGrowthSurvived != 0 {
			c.nurseryGrowthQuiet++
			if c.nurseryGrowthQuiet == 8 {
				c.nurseryGrowthSurvived = 0
				c.nurseryGrowthQuiet = 0
			}
		}
		return
	} else {
		c.nurseryGrowthQuiet = 0
		if c.nurseryGrowthSurvived < 2 {
			c.nurseryGrowthSurvived++
		}
		if c.nurseryGrowthSurvived < 2 {
			return
		}
	}
	if c.nurseryBump != 0 || c.survivorBump > c.survivorBytes {
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
	nextEden := eden * 2
	if denseNurseryPregrowEnabled && eden < denseNurseryPregrowBytes && uint64(youngObjects)*denseNurseryChargeBytes >= uint64(eden) {
		nextEden = min(c.nurseryGrowthLimit, denseNurseryPregrowBytes)
	}
	nextEden = min(nextEden, c.nurseryGrowthLimit)
	if nextEden <= eden {
		return
	}
	eden = nextEden
	survivor := min(eden/2, uint32(512<<10))
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
	c.nurseryGrowthSurvived = 0
	c.nurseryGrowthQuiet = 0
}
