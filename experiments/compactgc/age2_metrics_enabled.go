//go:build wago_gc_compact_age2 && wago_gc_compact_age2_metrics

package compactgc

// Untimed diagnostic only. Additional queue/start scans are NOT timing-neutral.
const age2MetricsEnabled = true

type Age2CycleMetrics struct {
	Full                                                           bool
	FreshCapacityBytes, FreshAllocatedBytes, FreshAllocatedObjects uint64
	RetainedPrefixInBytes, RetainedPrefixOutBytes                  uint64
	FirstSurvivorBytes, FirstSurvivorObjects                       uint64
	Age1PromotedBytes, Age1PromotedObjects                         uint64
	OverflowPromotedBytes, OverflowPromotedObjects                 uint64
	FullYoungLiveBytes, FullOldLiveBytes                           uint64
	OldDirectAllocatedBytes, OldDirectAllocatedObjects             uint64
	RememberedParents, RememberedEdges, RememberedOverflowSlots    uint64
}
type age2CycleMetric = Age2CycleMetrics
type Age2MetricsSnapshot struct {
	MinorSuccesses, FullSuccesses, MinorFailures, FullFailures uint64
	Last, MinorTotals, FullTotals                              Age2CycleMetrics
}
type age2MetricsState struct {
	age2Metrics          Age2MetricsSnapshot
	metricOldEnd         uint32
	metricOldAllocations uint64
}

func (c *Collector) Age2Metrics() (Age2MetricsSnapshot, bool) {
	if c == nil || !c.Age2Enabled() {
		return Age2MetricsSnapshot{}, false
	}
	return c.age2Metrics, true
}
func (d *age2CycleMetric) rememberedParent() { d.RememberedParents++ }
func (d *age2CycleMetric) rememberedEdge()   { d.RememberedEdges++ }
func (c *Collector) beginAge2Metric(full bool) age2CycleMetric {
	oldEnd := c.metricOldEnd
	if oldEnd == 0 {
		oldEnd = c.oldStart[c.active]
	}
	d := age2CycleMetric{Full: full, FreshCapacityBytes: uint64(c.nurseryEnd - c.survivorEnd), FreshAllocatedBytes: uint64(c.nurseryBump - c.survivorEnd), RetainedPrefixInBytes: uint64(c.survivorEnd - c.nurseryStart), OldDirectAllocatedBytes: uint64(c.oldBump - oldEnd), OldDirectAllocatedObjects: c.oldAllocations - c.metricOldAllocations}
	for i := c.survivorEnd / 8; i < c.nurseryBump/8; i++ {
		if c.starts[i] {
			d.FreshAllocatedObjects++
		}
	}
	if !full && c.rememberedOverflow {
		d.RememberedOverflowSlots = uint64((c.oldBump - c.oldStart[c.active]) / 8)
	}
	return d
}
func (c *Collector) captureAge2Metric(d *age2CycleMetric, count int, retained uint32) {
	d.RetainedPrefixOutBytes = uint64(retained)
	used := uint32(0)
	for _, r := range c.queue[:count] {
		h, e := c.layoutAt(uint32(r))
		if e != nil {
			panic("compactgc: validated age2 metric queue")
		}
		size := uint64(h.Stride)
		if d.Full {
			if c.young(r) {
				d.FullYoungLiveBytes += size
			} else {
				d.FullOldLiveBytes += size
			}
			continue
		}
		if uint32(r) < c.survivorEnd {
			d.Age1PromotedBytes += size
			d.Age1PromotedObjects++
			continue
		}
		d.FirstSurvivorBytes += size
		d.FirstSurvivorObjects++
		if h.Stride <= c.survivorCap-used {
			used += h.Stride
		} else {
			d.OverflowPromotedBytes += size
			d.OverflowPromotedObjects++
		}
	}
}
func (c *Collector) finishAge2Metric(d age2CycleMetric, err error) {
	if err != nil {
		if d.Full {
			c.age2Metrics.FullFailures++
		} else {
			c.age2Metrics.MinorFailures++
		}
		return
	}
	c.metricOldEnd = c.oldBump
	c.metricOldAllocations = c.oldAllocations
	c.age2Metrics.Last = d
	var t *Age2CycleMetrics
	if d.Full {
		c.age2Metrics.FullSuccesses++
		t = &c.age2Metrics.FullTotals
	} else {
		c.age2Metrics.MinorSuccesses++
		t = &c.age2Metrics.MinorTotals
	}
	t.Full = d.Full
	t.FreshCapacityBytes += d.FreshCapacityBytes
	t.FreshAllocatedBytes += d.FreshAllocatedBytes
	t.FreshAllocatedObjects += d.FreshAllocatedObjects
	t.RetainedPrefixInBytes += d.RetainedPrefixInBytes
	t.RetainedPrefixOutBytes += d.RetainedPrefixOutBytes
	t.FirstSurvivorBytes += d.FirstSurvivorBytes
	t.FirstSurvivorObjects += d.FirstSurvivorObjects
	t.Age1PromotedBytes += d.Age1PromotedBytes
	t.Age1PromotedObjects += d.Age1PromotedObjects
	t.OverflowPromotedBytes += d.OverflowPromotedBytes
	t.OverflowPromotedObjects += d.OverflowPromotedObjects
	t.FullYoungLiveBytes += d.FullYoungLiveBytes
	t.FullOldLiveBytes += d.FullOldLiveBytes
	t.OldDirectAllocatedBytes += d.OldDirectAllocatedBytes
	t.OldDirectAllocatedObjects += d.OldDirectAllocatedObjects
	t.RememberedParents += d.RememberedParents
	t.RememberedEdges += d.RememberedEdges
	t.RememberedOverflowSlots += d.RememberedOverflowSlots
}
