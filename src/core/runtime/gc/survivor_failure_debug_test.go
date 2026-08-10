//go:build wagodebug

package gc

import (
	"errors"
	"fmt"
	"testing"
)

func TestImmediatePromotionRunFailureRestoresExactState(t *testing.T) {
	for _, tc := range []struct {
		point failurePoint
		after int
	}{
		{point: failPromotionPlan},
		{point: failPromotionDestination},
		{point: failPromotionDestination, after: 1},
		{point: failPromotionCommit},
		{point: failPromotionCommit, after: 1},
	} {
		t.Run(fmt.Sprintf("point-%d-after-%d", tc.point, tc.after), func(t *testing.T) {
			c := newTestCollector(t, Config{
				NurseryBytes: 1024, DisableMovingNursery: true,
				ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096,
			})
			// Warm backing without advancing the old-space bump so the equal-size
			// promotion run takes its one-reservation path.
			c.throughput.mem = makeAlignedBytes(4096, 16)
			c.refreshNativeView()
			roots := make([]Root, 3)
			for i := range roots {
				ref, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				roots[i] = Root(ref)
			}
			before := snapshotPromotionState(c)
			cleanup := armFailure(c, tc.point, tc.after)
			err := c.CollectMinor(stressRootSlots(roots))
			cleanup()
			if !errors.Is(err, errInjectedFailure) {
				t.Fatalf("CollectMinor error=%v, want injected failure", err)
			}
			assertPromotionStateEqual(t, c, before)
		})
	}
}

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
