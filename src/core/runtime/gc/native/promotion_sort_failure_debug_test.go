//go:build wagodebug

package gc

import (
	"errors"
	"fmt"
	"testing"
)

func TestPromotionBucketsRollbackAndRetry(t *testing.T) {
	for _, moving := range []bool{false, true} {
		for _, point := range []failurePoint{failPromotionPlan, failPromotionDestination, failPromotionCommit} {
			for _, after := range []int{0, 17, 63} {
				t.Run(fmt.Sprintf("moving-%t/point-%d/after-%d", moving, point, after), func(t *testing.T) {
					cfg := Config{NurseryBytes: 64 << 10, ThroughputHeapBytes: 1 << 20, ThroughputPageBytes: 4096, VerifyAfterCollect: true}
					if moving {
						cfg.SurvivorBytes = 16 << 10
					}
					c := newTestCollector(t, cfg)
					roots := make([]Root, 64)
					for i := range roots {
						// Repeated, nonmonotonic sizes must take the stable-sort path.
						ref, err := c.NewArrayDefault(2, uint32(1+(i*43)%128))
						if err != nil {
							t.Fatal(err)
						}
						if err := c.ArraySet(ref, 0, I32Value(int32(i))); err != nil {
							t.Fatal(err)
						}
						if moving && i%2 == 0 {
							c.entry(ref).setYoungAge(1)
						}
						roots[i] = Root(ref)
					}
					before := snapshotPromotionState(c)
					cleanup := armFailure(c, point, after)
					err := c.CollectMinor(stressRootSlots(roots))
					cleanup()
					if !errors.Is(err, errInjectedFailure) {
						t.Fatalf("CollectMinor=%v, want injected failure", err)
					}
					assertPromotionStateEqual(t, c, before)
					if err := c.CollectMinor(stressRootSlots(roots)); err != nil {
						t.Fatalf("retry: %v", err)
					}
					if nativeCollectorFastPaths && cap(c.promotionSortScratch) == 0 {
						t.Fatal("collection did not exercise promotion buckets")
					}
					for i, root := range roots {
						value, err := c.ArrayGet(Ref(root), 0)
						if err != nil || value.Bits != uint64(i) {
							t.Fatalf("array %d after retry=%+v,%v", i, value, err)
						}
					}
				})
			}
		}
	}
}
