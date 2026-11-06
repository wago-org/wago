//go:build wagodebug && !tinygo && !wago_target_tinygo

package gc

import (
	"errors"
	"testing"
)

func TestParallelPromotionFailuresRemainTransactional(t *testing.T) {
	parallelTestProcessors(t)
	for _, point := range []failurePoint{failPromotionPlan, failPromotionDestination, failPromotionCommit} {
		for _, immediate := range []bool{false, true} {
			c, parent, _ := newParallelTestGraph(t, 4, immediate)
			roots := RefSliceRoots{parent}
			before := snapshotPromotionState(c)
			cleanup := armFailure(c, point, 17)
			err := c.CollectMinor(roots)
			cleanup()
			if !errors.Is(err, errInjectedFailure) {
				t.Fatalf("point=%d immediate=%v: %v", point, immediate, err)
			}
			if c.parallel == nil {
				t.Fatal("failure test missed parallel tracing")
			}
			assertPromotionStateEqual(t, c, before)
			if err = c.CollectMinor(roots); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if err = c.Verify(roots); err != nil {
				t.Fatal(err)
			}
			c.Close()
		}
	}
}
