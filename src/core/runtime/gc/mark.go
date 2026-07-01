package gc

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
