package gc

import (
	"cmp"
	"fmt"
	"math/rand"
	"slices"
	"testing"
)

// This is the original sort key. Keep the oracle independent of bucket indices.
func referencePromotionSort(c *Collector, plans []plannedPromotion) {
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

func promotionSortFixture(n int, classLimit uint32, mixed bool) (*Collector, []plannedPromotion) {
	c := &Collector{handles: make([]handleEntry, n+1)}
	c.throughput.classLimit = classLimit
	plans := make([]plannedPromotion, n)
	rng := rand.New(rand.NewSource(7391))
	for i := range plans {
		size := uint32(16 + 8*rng.Intn(32))
		if mixed {
			size = uint32(16 + 8*rng.Intn(6000))
		}
		h := uint32(i + 1)
		c.handles[h] = handleEntry{size: size, space: spaceNursery}
		plans[i] = plannedPromotion{handle: h}
		if mixed {
			switch i % 11 {
			case 0:
				plans[i].entry = handleEntry{space: spaceNursery, off: h * 16, size: size}
			case 1:
				plans[i].entry = handleEntry{space: spaceLarge, off: h * 16, size: size}
			}
		}
	}
	return c, plans
}

func TestPromotionSortMatchesStableOrder(t *testing.T) {
	for _, limit := range []uint32{32, 128, 4096, 32768} {
		for _, n := range []int{0, 1, 17, 32, 127, 4096, maxPromotionSortPlans, maxPromotionSortPlans + 1} {
			t.Run(fmt.Sprintf("limit-%d/plans-%d", limit, n), func(t *testing.T) {
				c, plans := promotionSortFixture(n, limit, true)
				want := slices.Clone(plans)
				referencePromotionSort(c, want)
				c.sortPromotionPlans(plans)
				if !slices.Equal(plans, want) {
					for i := range plans {
						if plans[i] != want[i] {
							t.Fatalf("plan %d: got %+v, want %+v (stable key order)", i, plans[i], want[i])
						}
					}
				}
				if cap(c.promotionSortScratch) > maxPromotionSortPlans {
					t.Fatalf("retained %d scratch plans, limit %d", cap(c.promotionSortScratch), maxPromotionSortPlans)
				}
				if n > maxPromotionSortPlans && cap(c.promotionSortScratch) != 0 {
					t.Fatalf("oversized input allocated scratch: cap=%d", cap(c.promotionSortScratch))
				}
			})
		}
	}
}

func TestPromotionSortReusesAndClearsScratch(t *testing.T) {
	c, seed := promotionSortFixture(4096, 4096, false)
	plans := slices.Clone(seed)
	c.sortPromotionPlans(plans)
	allocs := testing.AllocsPerRun(10, func() {
		copy(plans, seed)
		c.sortPromotionPlans(plans)
	})
	if allocs != 0 {
		t.Fatalf("warm sort allocated %g times", allocs)
	}
	for i, plan := range c.promotionSortScratch[:cap(c.promotionSortScratch)] {
		if plan != (plannedPromotion{}) {
			t.Fatalf("scratch plan %d retains %+v", i, plan)
		}
	}
}

func BenchmarkPromotionPlanSort(b *testing.B) {
	for _, mixed := range []bool{false, true} {
		for _, n := range []int{32, 64, 4096, 32768} {
			for _, mode := range []string{"original", "cached", "buckets"} {
				b.Run(fmt.Sprintf("mixed-%t/plans-%d/%s", mixed, n, mode), func(b *testing.B) {
					c, seed := promotionSortFixture(n, 4096, mixed)
					for i := range seed {
						if seed[i].entry.space == spaceFree {
							seed[i].entry.size = c.throughput.promotionAllocSize(c.handles[seed[i].handle].size)
						}
					}
					plans := slices.Clone(seed)
					c.sortPromotionPlans(plans)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						copy(plans, seed)
						if mode == "original" {
							referencePromotionSort(c, plans)
						} else if mode == "cached" {
							slices.SortStableFunc(plans, func(a, b plannedPromotion) int {
								ak, bk := promotionPlanKind(a.entry), promotionPlanKind(b.entry)
								if ak != bk {
									return cmp.Compare(ak, bk)
								}
								if ak == 1 {
									return cmp.Compare(a.entry.size, b.entry.size)
								}
								return 0
							})
						} else {
							c.sortPromotionPlans(plans)
						}
					}
				})
			}
		}
	}
}
