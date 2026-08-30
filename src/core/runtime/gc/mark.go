package gc

import "time"

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
	rootMarkTinyCount
	rootMarkTinyBounded
)

// VisitRootRef implements RootRefSink. Collection is synchronous per Collector,
// so the active mark mode can live in the collector instead of an escaping
// closure allocated once per collection.
func (c *Collector) VisitRootRef(r Ref) bool {
	if c.rootMarkMode == rootMarkTinyCount || c.rootMarkMode == rootMarkTinyBounded {
		c.tinyGC.lastStepWork.refSlots++
		if c.rootMarkMode == rootMarkTinyCount {
			return true
		}
	}
	if c.telemetryEnabled() {
		c.cfg.Telemetry.noteRoot(c.telemetryRootClass)
	}
	switch c.rootMarkMode {
	case rootMarkFull:
		c.markRef(r)
	case rootMarkNursery:
		c.markNurseryRef(r)
	case rootMarkTiny, rootMarkTinyBounded:
		c.tinyMarkRef(r)
	}
	return true
}

// VisitClassifiedRootRef implements ClassifiedRootRefSink. The direct
// integration is telemetry-only, so per-class timing can remain out of release
// builds and ordinary root walks.
func (c *Collector) VisitClassifiedRootRef(class RootClass, r Ref) bool {
	if !c.telemetryEnabled() {
		return c.VisitRootRef(r)
	}
	if c.rootMarkMode == rootMarkTinyCount || c.rootMarkMode == rootMarkTinyBounded {
		c.tinyGC.lastStepWork.refSlots++
		if c.rootMarkMode == rootMarkTinyCount {
			return true
		}
	}
	if class >= rootClassCount {
		class = RootNativeFrame
	}
	start := time.Now()
	previousClass := c.telemetryRootClass
	previousPhase := c.cfg.Telemetry.active.phase
	c.telemetryRootClass = class
	if class == RootNativeFrame {
		c.cfg.Telemetry.setPhase(telemetryPhaseNativeRoots)
	} else {
		c.cfg.Telemetry.setPhase(telemetryPhasePersistentRoots)
	}
	c.cfg.Telemetry.noteRoot(class)
	c.markRootForMode(r, c.rootMarkMode)
	c.cfg.Telemetry.addRootTime(class, uint64(time.Since(start)))
	c.cfg.Telemetry.setPhase(previousPhase)
	c.telemetryRootClass = previousClass
	return true
}

func (c *Collector) finishDirectRootMark() {
	c.rootMarkMode = 0
	c.telemetryRootClass = RootNativeFrame
}

func (c *Collector) markDirectRoots(roots DirectRootRefSet, mode uint8) {
	c.rootMarkMode = mode
	defer c.finishDirectRootMark()
	roots.RangeRootRefs(c)
}

func (c *Collector) markRoots(roots RootSet) {
	if !c.telemetryEnabled() {
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
		return
	}
	c.enumerateRoots(roots, rootMarkFull)
	c.drainMarkStack()
}

func (c *Collector) enumerateRoots(roots RootSet, mode uint8) {
	if !c.telemetryEnabled() {
		c.markUnclassifiedRoots(roots, mode)
		c.markPersistentRoots(mode, false)
		return
	}
	c.markTelemetryRootSet(roots, RootNativeFrame, mode)
	c.markPersistentRoots(mode, true)
}

func (c *Collector) markNurseryRoots(roots RootSet) {
	if !c.telemetryEnabled() {
		if direct, ok := roots.(DirectRootRefSet); ok {
			c.markDirectRoots(direct, rootMarkNursery)
		} else if roots != nil && !rangeRootRefs(roots, func(r Ref) bool { c.markNurseryRef(r); return true }) {
			roots.RangeRoots(func(s RootSlot) bool { c.markNurseryRef(s.GetRef()); return true })
		}
		c.markDirtyPersistentRoots(false)
		return
	}
	c.markTelemetryRootSet(roots, RootNativeFrame, rootMarkNursery)
	c.markDirtyPersistentRoots(true)
}

// markDirtyPersistentRoots uses stable slot-card indexes as the authoritative
// Throughput minor-GC root input. Full and Tiny collections still enumerate all
// persistent roots.
func (c *Collector) markDirtyPersistentRoots(measured bool) {
	if c.cardFallback {
		c.markPersistentRoots(rootMarkNursery, measured)
		return
	}
	previousPhase := telemetryPhaseNone
	if measured {
		previousPhase = c.cfg.Telemetry.active.phase
		c.cfg.Telemetry.setPhase(telemetryPhasePersistentRoots)
		defer c.cfg.Telemetry.setPhase(previousPhase)
	}
	for _, card := range c.slotCards {
		var r Ref
		class := RootTable
		switch card.kind {
		case SlotGlobal:
			if !slotIndexOK(card.index, len(c.globalSlots)) {
				continue
			}
			r = c.globalSlots[card.index]
			if measured {
				class = c.cfg.Telemetry.globalRootClass(card.index)
			} else {
				class = RootGlobal
			}
		case SlotTable:
			if !slotIndexOK(card.index, len(c.tableSlots)) {
				continue
			}
			r = c.tableSlots[card.index]
		default:
			continue
		}
		if !measured {
			c.markNurseryRef(r)
			continue
		}
		start := time.Now()
		c.cfg.Telemetry.noteRoot(class)
		c.markNurseryRef(r)
		c.cfg.Telemetry.addRootTime(class, uint64(time.Since(start)))
	}
}

func (c *Collector) markUnclassifiedRoots(roots RootSet, mode uint8) {
	if direct, ok := roots.(DirectRootRefSet); ok {
		c.markDirectRoots(direct, mode)
		return
	}
	switch mode {
	case rootMarkNursery:
		if roots != nil && !rangeRootRefs(roots, func(r Ref) bool { c.markNurseryRef(r); return true }) {
			roots.RangeRoots(func(s RootSlot) bool { c.markNurseryRef(s.GetRef()); return true })
		}
	case rootMarkTiny:
		if roots != nil && !rangeRootRefs(roots, func(r Ref) bool { c.tinyMarkRef(r); return true }) {
			roots.RangeRoots(func(s RootSlot) bool { c.tinyMarkRef(s.GetRef()); return true })
		}
	default:
		if roots != nil && !rangeRootRefs(roots, func(r Ref) bool { c.markRef(r); return true }) {
			roots.RangeRoots(func(s RootSlot) bool { c.markRef(s.GetRef()); return true })
		}
	}
}

func (c *Collector) markTelemetryRootSet(roots RootSet, class RootClass, mode uint8) {
	if roots == nil {
		return
	}
	if direct, ok := roots.(DirectClassifiedRootRefSet); ok {
		c.rootMarkMode = mode
		defer c.finishDirectRootMark()
		direct.RangeClassifiedRootRefs(c)
		return
	}
	switch groups := roots.(type) {
	case RootGroups:
		for _, group := range groups {
			c.markTelemetryRootSet(group.Roots, group.Class, mode)
		}
		return
	case ClassifiedRoots:
		c.markTelemetryRootSet(groups.Roots, groups.Class, mode)
		return
	}
	start := time.Now()
	previous := c.telemetryRootClass
	previousPhase := c.cfg.Telemetry.active.phase
	c.telemetryRootClass = class
	if class == RootNativeFrame {
		c.cfg.Telemetry.setPhase(telemetryPhaseNativeRoots)
	} else {
		c.cfg.Telemetry.setPhase(telemetryPhasePersistentRoots)
	}
	defer func() {
		c.cfg.Telemetry.addRootTime(class, uint64(time.Since(start)))
		c.cfg.Telemetry.setPhase(previousPhase)
		c.telemetryRootClass = previous
	}()
	mark := c.markRef
	if mode == rootMarkNursery {
		mark = c.markNurseryRef
	} else if mode == rootMarkTiny {
		mark = c.tinyMarkRef
	}
	if direct, ok := roots.(DirectRootRefSet); ok {
		c.markDirectRoots(direct, mode)
		return
	}
	if !rangeRootRefs(roots, func(r Ref) bool {
		c.cfg.Telemetry.noteRoot(class)
		mark(r)
		return true
	}) {
		roots.RangeRoots(func(s RootSlot) bool {
			c.cfg.Telemetry.noteRoot(class)
			mark(s.GetRef())
			return true
		})
	}
}

func (c *Collector) markPersistentRoots(mode uint8, measured bool) {
	previousPhase := telemetryPhaseNone
	if measured {
		previousPhase = c.cfg.Telemetry.active.phase
		c.cfg.Telemetry.setPhase(telemetryPhasePersistentRoots)
		defer c.cfg.Telemetry.setPhase(previousPhase)
	}
	for i, r := range c.globalSlots {
		if !measured {
			c.markRootForMode(r, mode)
			continue
		}
		start := time.Now()
		class := c.cfg.Telemetry.globalRootClass(uint32(i))
		c.cfg.Telemetry.noteRoot(class)
		c.markRootForMode(r, mode)
		c.cfg.Telemetry.addRootTime(class, uint64(time.Since(start)))
	}
	for _, r := range c.tableSlots {
		if !measured {
			c.markRootForMode(r, mode)
			continue
		}
		start := time.Now()
		c.cfg.Telemetry.noteRoot(RootTable)
		c.markRootForMode(r, mode)
		c.cfg.Telemetry.addRootTime(RootTable, uint64(time.Since(start)))
	}
}

func (c *Collector) markRootForMode(r Ref, mode uint8) {
	switch mode {
	case rootMarkNursery:
		c.markNurseryRef(r)
	case rootMarkTiny, rootMarkTinyBounded:
		c.tinyMarkRef(r)
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
