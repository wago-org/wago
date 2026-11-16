package directgc

// Retirement happens only after successful preflight, copying and edge/root
// rewriting. No source start bit is needed after this point. Only promote-all
// mode uses this helper: age-bearing survivor copies need separate accounting.
// Previous-cycle density is deliberately conservative: only <=32-byte mean
// stride has positive microbenchmark evidence. Workload changes incur one-cycle
// prediction lag. Empty retirement resets prediction rather than dividing by0.
func (c *Collector) retireUsingMap() bool {
	return retireStartMap || (retireAdaptive && c.retirementDense)
}
func (c *Collector) retireNursery() {
	var count uint64
	if c.retireUsingMap() {
		count = c.retireNurseryMap()
	} else {
		count = c.retireNurseryHeaders()
	}
	c.retiredNurseryAllocations += count
	if retireAdaptive {
		c.retirementDense = count != 0 && uint64(c.nurseryBump-c.nurseryStart) <= count*32
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
