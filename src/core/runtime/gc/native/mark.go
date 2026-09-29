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
func (c *Collector) clearNurseryMarks() {
	if len(c.mark) < len(c.handles) {
		c.mark = make([]bool, len(c.handles))
	}
	for _, h := range c.nurseryHandles {
		if h != 0 && int(h) < len(c.mark) {
			c.mark[h] = false
		}
	}
	c.markStack = c.markStack[:0]
}

const (
	rootMarkFull uint8 = iota + 1
	rootMarkNursery
	rootMarkTiny
	rootMarkTinyStage
)

// VisitRootRef implements RootRefSink. Collection is synchronous per Collector,
// so the active mark mode can live in the collector instead of an escaping
// closure allocated once per collection.
func (c *Collector) VisitRootRef(r Ref) bool {
	switch c.rootMarkMode {
	case rootMarkFull:
		c.markRef(r)
	case rootMarkNursery:
		c.markNurseryRef(r)
	case rootMarkTiny:
		c.tinyMarkRef(r)
	case rootMarkTinyStage:
		c.tinyStageRootRef(r)
	}
	return true
}

func (c *Collector) finishDirectRootMark() {
	c.rootMarkMode = 0
}

func (c *Collector) markDirectRoots(roots DirectRootRefSet, mode uint8) {
	c.rootMarkMode = mode
	defer c.finishDirectRootMark()
	roots.RangeRootRefs(c)
}

func (c *Collector) markRoots(roots RootSet) {
	if direct, ok := roots.(DirectRootRefSet); ok {
		c.markDirectRoots(direct, rootMarkFull)
	} else if roots != nil && !rangeRootRefs(roots, func(r Ref) bool { c.markRef(r); return true }) {
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

func (c *Collector) markNurseryRoots(roots RootSet) {
	if direct, ok := roots.(DirectRootRefSet); ok {
		c.markDirectRoots(direct, rootMarkNursery)
	} else if roots != nil && !rangeRootRefs(roots, func(r Ref) bool { c.markNurseryRef(r); return true }) {
		roots.RangeRoots(func(s RootSlot) bool { c.markNurseryRef(s.GetRef()); return true })
	}
	c.markDirtyPersistentRoots()
}

// markDirtyPersistentRoots uses stable slot-card indexes as the authoritative
// Throughput minor-GC root input. Full and Tiny collections still enumerate all
// persistent roots.
func (c *Collector) markDirtyPersistentRoots() {
	if c.cardFallback {
		c.markPersistentRoots(rootMarkNursery)
		return
	}
	for _, card := range c.slotCards {
		var r Ref
		switch card.kind {
		case SlotGlobal:
			if !slotIndexOK(card.index, len(c.globalSlots)) {
				continue
			}
			r = c.globalSlots[card.index]
		case SlotTable:
			if !slotIndexOK(card.index, len(c.tableSlots)) {
				continue
			}
			r = c.tableSlots[card.index]
		default:
			continue
		}
		c.markNurseryRef(r)
	}
}

func (c *Collector) markPersistentRoots(mode uint8) {
	for _, r := range c.globalSlots {
		c.markRootForMode(r, mode)
	}
	for _, r := range c.tableSlots {
		c.markRootForMode(r, mode)
	}
}

func (c *Collector) markRootForMode(r Ref, mode uint8) {
	switch mode {
	case rootMarkNursery:
		c.markNurseryRef(r)
	case rootMarkTiny:
		c.tinyMarkRef(r)
	case rootMarkTinyStage:
		c.tinyStageRootRef(r)
	default:
		c.markRef(r)
	}
}

func (c *Collector) drainNurseryMarkStack() uint64 {
	var scanned uint64
	for len(c.markStack) > 0 {
		n := len(c.markStack) - 1
		h := c.markStack[n]
		c.markStack = c.markStack[:n]
		c.stats.MinorObjectsScanned++
		scanned++
		c.scanObjectRefs(h, c.markNurseryRef)
	}
	return scanned
}

func (c *Collector) markNurseryRef(r Ref) {
	if !r.IsObj() {
		return
	}
	h := handleOf(r)
	if h == 0 || int(h) >= len(c.handles) || !c.handles[h].young() || c.mark[h] {
		return
	}
	c.mark[h] = true
	c.markStack = append(c.markStack, h)
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

func (c *Collector) VisitClassifiedRootRef(_ RootClass, r Ref) bool { return c.VisitRootRef(r) }
