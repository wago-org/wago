//go:build wagodebug && (amd64 || arm64)

package gc

import (
	"errors"
	"fmt"
	"testing"
)

func TestSparseMajorMarksAfterFailedPromotion(t *testing.T) {
	for _, point := range []failurePoint{failPromotionPlan, failPromotionDestination, failPromotionCommit} {
		t.Run(fmt.Sprint(point), func(t *testing.T) {
			c := newMajorBlocksEmptyCollector(t, 8191, false)
			c.tenuringThreshold = 1
			if !c.PrepareNativeStructHandles() {
				t.Fatal("native reservation failed")
			}
			roots := publishMajorBlocksNative(c, 4)
			// Leave marks behind from a full cycle, then enter a transactional
			// minor failure with the sparse occupancy map still active.
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			cleanup := armFailure(c, point, 0)
			err := c.CollectMinor(roots)
			cleanup()
			if !errors.Is(err, errInjectedFailure) {
				t.Fatalf("minor error = %v, want injected failure", err)
			}
			if err := c.CollectFull(roots[:1]); err != nil {
				t.Fatal(err)
			}
			if got := c.Stats().LiveObjects; got != 1 {
				t.Fatalf("full collection after rollback retained %d objects, want 1", got)
			}
			if err := c.CollectMinor(roots[:1]); err != nil {
				t.Fatal(err)
			}
			if err := c.Verify(roots[:1]); err != nil {
				t.Fatal(err)
			}
			if err := c.CollectFull(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			if got := c.Stats().LiveObjects; got != 0 {
				t.Fatalf("post-rollback marks retained %d unreachable objects", got)
			}
		})
	}
}
