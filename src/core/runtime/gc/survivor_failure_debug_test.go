//go:build wagodebug

package gc

import (
	"errors"
	"fmt"
	"testing"
)

func TestSurvivorMovementFailureRestoresExactState(t *testing.T) {
	for _, point := range []failurePoint{failPromotionPlan, failPromotionDestination, failPromotionCommit} {
		t.Run(fmt.Sprintf("point-%d", point), func(t *testing.T) {
			c := newTestCollector(t, Config{
				NurseryBytes: 1024, SurvivorBytes: 512,
				ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096,
			})
			roots := make([]Root, 2)
			for i := range roots {
				ref, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				roots[i] = Root(ref)
			}
			before := snapshotPromotionState(c)
			cleanup := armFailure(c, point, 0)
			err := c.CollectMinor(stressRootSlots(roots))
			cleanup()
			if !errors.Is(err, errInjectedFailure) {
				t.Fatalf("CollectMinor error=%v, want injected failure", err)
			}
			assertPromotionStateEqual(t, c, before)
		})
	}
}
