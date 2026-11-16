//go:build wago_gc_compact_shadow

package compactgc

// Shadow diagnostics are untimed, read-only graph observations, never collector
// decisions. Their scratch is disjoint from the collector's preflight scratch.
const shadowDiagnostics = true

type ShadowCycle struct {
	Covered, Valid                   bool
	PromotedBytes, PromotedObjects   uint64
	ReachableBytes, ReachableObjects uint64
}
type ShadowSnapshot struct {
	ScratchBytes                                               uint64
	MinorSuccesses, MinorFailures, FullSuccesses, FullFailures uint64
	CoveredCycles, InvalidCycles, UncoveredCycles              uint64
	Last, Totals                                               ShadowCycle
}
type shadowDiagnosticState struct {
	shadow                 ShadowSnapshot
	shadowMark             []uint64
	shadowQueue            []Ref
	shadowStart, shadowEnd uint32
	shadowKnown            bool
}
type shadowCycle struct {
	observation ShadowCycle
	full        bool
	oldBefore   uint32
}

func (c *Collector) ShadowDiagnostics() ShadowSnapshot {
	s := c.shadow
	s.ScratchBytes = uint64(len(c.shadowMark))*8 + uint64(len(c.shadowQueue))*4
	return s
}

func (c *Collector) beginShadowCycle(roots []*Ref, full bool) shadowCycle {
	d := shadowCycle{full: full, oldBefore: c.oldBump}
	if full {
		return d
	}
	if !c.shadowKnown {
		return d
	}
	d.observation.Covered = true
	// Allocation here belongs only to the diagnostic, not collector timings.
	if len(c.shadowMark) != len(c.mark) {
		c.shadowMark = make([]uint64, len(c.mark))
		c.shadowQueue = make([]Ref, len(c.queue))
	}
	clear(c.shadowMark)
	clear(c.shadowQueue)
	defer func() { clear(c.shadowMark); clear(c.shadowQueue) }()
	if c.shadowStart < c.oldStart[c.active] || c.shadowEnd > c.oldBump || c.shadowEnd < c.shadowStart {
		return d
	}
	for off := c.shadowStart; off < c.shadowEnd; {
		if !c.valid(Ref(off)) {
			return d
		}
		h, e := c.layoutAt(off)
		if e != nil || h.Stride > c.shadowEnd-off {
			return d
		}
		d.observation.PromotedBytes += uint64(h.Stride)
		d.observation.PromotedObjects++
		off += h.Stride
	}
	count := 0
	visit := func(r Ref) error {
		if r == 0 || r.IsI31() {
			return nil
		}
		if !c.valid(r) {
			return ErrRef
		}
		slot := uint32(r) / 16
		mask := uint64(1) << (slot % 64)
		if c.shadowMark[slot/64]&mask != 0 {
			return nil
		}
		h, e := c.layoutAt(uint32(r))
		if e != nil {
			return e
		}
		if count >= len(c.shadowQueue) {
			return ErrOOM
		}
		c.shadowMark[slot/64] |= mask
		c.shadowQueue[count] = r
		count++
		if uint32(r) >= c.shadowStart && uint32(r) < c.shadowEnd {
			d.observation.ReachableBytes += uint64(h.Stride)
			d.observation.ReachableObjects++
		}
		return nil
	}
	for _, p := range roots {
		if p == nil || visit(*p) != nil {
			return d
		}
	}
	for i := 0; i < count; i++ {
		if e := c.eachRef(uint32(c.shadowQueue[i]), func(at uint32) error { return visit(Ref(c.u32(at))) }); e != nil {
			return d
		}
	}
	d.observation.Valid = true
	return d
}
func (c *Collector) finishShadowCycle(d shadowCycle, err error) {
	if d.full {
		// Even a failed full attempt ends the adjacent-minor observation sequence.
		c.shadowKnown = false
		if err != nil {
			c.shadow.FullFailures++
		} else {
			c.shadow.FullSuccesses++
		}
		return
	}
	if err != nil {
		c.shadow.MinorFailures++
		return
	}
	c.shadow.MinorSuccesses++
	c.shadow.Last = d.observation
	if !d.observation.Covered {
		c.shadow.UncoveredCycles++
	} else if !d.observation.Valid {
		c.shadow.InvalidCycles++
	} else {
		c.shadow.CoveredCycles++
		c.shadow.Totals.PromotedBytes += d.observation.PromotedBytes
		c.shadow.Totals.PromotedObjects += d.observation.PromotedObjects
		c.shadow.Totals.ReachableBytes += d.observation.ReachableBytes
		c.shadow.Totals.ReachableObjects += d.observation.ReachableObjects
	}
	// Only a successful minor establishes the next cohort, excluding old-space
	// oversized allocations made between this and the next collection.
	c.shadowStart = d.oldBefore
	c.shadowEnd = c.oldBump
	c.shadowKnown = true
}
