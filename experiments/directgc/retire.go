package directgc

// Retirement happens only after successful preflight, copying and edge/root
// rewriting. No source start bit is needed after this point. Only promote-all
// mode uses this helper: age-bearing survivor copies need separate accounting.
func (c *Collector) retireNursery() {
	if retireStartMap {
		c.retiredNurseryAllocations += c.retireNurseryMap()
	} else {
		c.retiredNurseryAllocations += c.retireNurseryHeaders()
	}
}
func (c *Collector) retireNurseryHeaders() (count uint64) {
	for off := c.nurseryStart; off < c.nurseryBump; off += c.stride(off) {
		c.starts[off/8] = false
		count++
	}
	return
}
func (c *Collector) retireNurseryMap() (count uint64) {
	region := c.starts[c.nurseryStart/8 : c.nurseryBump/8]
	for _, yes := range region {
		if yes {
			count++
		}
	}
	clear(region)
	return
}
