//go:build wago_gc_compact_cohort

package compactgc

// This diagnostic deliberately adds traversal work. Never use its timing as a
// collector benchmark. No diagnostic callback can mutate the heap or roots.
const cohortDiagnostics = true

type CohortCycle struct {
	Full                                           bool
	YoungAllocatedBytes, YoungAllocatedObjects     uint64
	OldOccupiedBytes                               uint64
	YoungLiveBytes, YoungLiveObjects               uint64
	OldLiveBytes, OldLiveObjects                   uint64
	OversizedOldLiveBytes, OversizedOldLiveObjects uint64
}
type cohortCycle = CohortCycle

// CohortSnapshot separates committed measurements from failed attempts. Last
// contains only a successful collection. Totals are split by collection kind.
type CohortSnapshot struct {
	MinorSuccesses, FullSuccesses, MinorFailures, FullFailures uint64
	OversizedAllocatedBytes, OversizedAllocatedObjects         uint64
	Last, MinorTotals, FullTotals                              CohortCycle
}
type cohortDiagnosticState struct{ cohort CohortSnapshot }

func (c *Collector) CohortDiagnostics() CohortSnapshot { return c.cohort }
func (c *Collector) recordOversized(bytes uint32) {
	c.cohort.OversizedAllocatedBytes += uint64(bytes)
	c.cohort.OversizedAllocatedObjects++
}
func (c *Collector) beginCohortCycle(full bool) cohortCycle {
	d := cohortCycle{Full: full, YoungAllocatedBytes: uint64(c.nurseryBump - c.nurseryStart), OldOccupiedBytes: uint64(c.oldBump - c.oldStart[c.active])}
	for i := c.nurseryStart / 8; i < c.nurseryBump/8; i++ {
		if c.starts[i] {
			d.YoungAllocatedObjects++
		}
	}
	return d
}
func (c *Collector) captureCohortLive(d *cohortCycle, count int) {
	for _, r := range c.queue[:count] {
		h, err := c.layoutAt(uint32(r))
		if err != nil {
			panic("compactgc: validated diagnostic queue")
		}
		if c.young(r) {
			d.YoungLiveBytes += uint64(h.Stride)
			d.YoungLiveObjects++
		} else {
			d.OldLiveBytes += uint64(h.Stride)
			d.OldLiveObjects++
			if h.Stride > c.nurseryEnd-c.nurseryStart {
				d.OversizedOldLiveBytes += uint64(h.Stride)
				d.OversizedOldLiveObjects++
			}
		}
	}
}
func (c *Collector) finishCohortCycle(d cohortCycle, err error) {
	if err != nil {
		if d.Full {
			c.cohort.FullFailures++
		} else {
			c.cohort.MinorFailures++
		}
		return
	}
	c.cohort.Last = d
	var t *CohortCycle
	if d.Full {
		c.cohort.FullSuccesses++
		t = &c.cohort.FullTotals
	} else {
		c.cohort.MinorSuccesses++
		t = &c.cohort.MinorTotals
	}
	t.Full = d.Full
	t.YoungAllocatedBytes += d.YoungAllocatedBytes
	t.YoungAllocatedObjects += d.YoungAllocatedObjects
	t.OldOccupiedBytes += d.OldOccupiedBytes
	t.YoungLiveBytes += d.YoungLiveBytes
	t.YoungLiveObjects += d.YoungLiveObjects
	t.OldLiveBytes += d.OldLiveBytes
	t.OldLiveObjects += d.OldLiveObjects
	t.OversizedOldLiveBytes += d.OversizedOldLiveBytes
	t.OversizedOldLiveObjects += d.OversizedOldLiveObjects
}
