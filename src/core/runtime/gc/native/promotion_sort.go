package gc

import (
	"cmp"
	"slices"
)

// A plan has no Go pointers. Bound the extra retained storage to 1.25 MiB
// (65536 * 20 bytes); larger collections keep the in-place stable sort.
const maxPromotionSortPlans = 65536
const minPromotionSortPlans = 64

// sortPromotionPlans preserves the existing stable (kind, allocation-size)
// order, including the order of equal-size allocations and failure points.
// Most objects occupy one of a fixed number of old-space size classes, so
// counting buckets avoid comparison-sort swaps. Only oversized allocations
// still need a comparison sort. No object or allocation state is changed here.
func (c *Collector) sortPromotionPlans(plans []plannedPromotion) {
	if !nativeCollectorFastPaths || len(plans) < minPromotionSortPlans || len(plans) > maxPromotionSortPlans {
		c.sortPromotionPlansByComparison(plans)
		return
	}
	const overflow = len(throughputClassSizes) + 1
	const large = overflow + 1
	var next [large + 1]int
	for _, p := range plans {
		next[c.promotionSortBucket(p)]++
	}
	if next[overflow] > len(plans)/2 {
		c.sortPromotionPlansByComparison(plans)
		return
	}
	total := 0
	for i, count := range next {
		next[i] = total
		total += count
	}
	overflowStart, overflowEnd := next[overflow], next[large]
	if cap(c.promotionSortScratch) < len(plans) {
		capacity := max(len(plans), min(maxPromotionSortPlans, 2*cap(c.promotionSortScratch)))
		c.promotionSortScratch = make([]plannedPromotion, capacity)
	}
	scratch := c.promotionSortScratch[:len(plans)]
	for _, p := range plans {
		bucket := c.promotionSortBucket(p)
		scratch[next[bucket]] = p
		next[bucket]++
	}
	copy(plans, scratch)
	clear(scratch)
	c.promotionSortScratch = c.promotionSortScratch[:0]
	if overflowEnd-overflowStart > 1 {
		c.sortPromotionPlansByComparison(plans[overflowStart:overflowEnd])
	}
}

func (c *Collector) promotionSortBucket(p plannedPromotion) int {
	switch p.entry.space {
	case spaceNursery:
		return 0
	case spaceFree:
		size := Align16(c.handles[p.handle].size)
		if size <= c.throughput.classLimit {
			if class := c.throughput.classFor(size); class >= 0 {
				return class + 1
			}
		}
		return len(throughputClassSizes) + 1
	default:
		return len(throughputClassSizes) + 2
	}
}

func (c *Collector) sortPromotionPlansByComparison(plans []plannedPromotion) {
	slices.SortStableFunc(plans, func(a, b plannedPromotion) int {
		ak, bk := promotionPlanKind(a.entry), promotionPlanKind(b.entry)
		if ak != bk {
			return cmp.Compare(ak, bk)
		}
		if ak == 1 {
			return cmp.Compare(c.throughput.promotionAllocSize(c.handles[a.handle].size), c.throughput.promotionAllocSize(c.handles[b.handle].size))
		}
		return 0
	})
}
