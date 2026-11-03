//go:build wagodebug

package gc

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

func TestRunClaimsRollbackAndRetry(t *testing.T) {
	for workers := 2; workers <= 8; workers++ {
		for _, optional := range []bool{false, true} {
			points := []failurePoint{failPromotionDestination, failPromotionCommit}
			if optional {
				points = []failurePoint{failEvacuationCopy, failEvacuationCommit}
			}
			for _, point := range points {
				t.Run(fmt.Sprintf("workers%d/optional%t/point%d", workers, optional, point), func(t *testing.T) {
					c, all, objects, s := newAsymmetricRunCollector(t, workers, optional)
					h := &c.throughput
					before := append([]handleEntry(nil), c.handles...)
					free := h.freeSpans()
					bump := h.bump
					payload := make([][]byte, len(objects))
					for i, r := range objects {
						payload[i] = bytes.Clone(c.bytes(r))
					}
					after := 0
					if point == failPromotionDestination {
						after = 100
					} // Beyond old lane exhaustion.
					disarm := armFailure(c, point, after)
					err := collectWithAdmittedRuns(c, objects, s, optional)
					disarm()
					if err == nil {
						t.Fatal("failure hook not reached")
					}
					if !reflect.DeepEqual(before, c.handles) || !reflect.DeepEqual(free, h.freeSpans()) || bump != h.bump {
						t.Fatal("rollback ownership")
					}
					for i, r := range objects {
						if !bytes.Equal(payload[i], c.bytes(r)) {
							t.Fatal("source changed")
						}
					}
					if err := c.Verify(all); err != nil {
						t.Fatal(err)
					}
					if err := collectWithAdmittedRuns(c, objects, s, optional); err != nil {
						t.Fatal("retry", err)
					}
					if optional && c.CollectionDiagnostics().Evacuation != "moved" {
						t.Fatal("retry did not move")
					}
					for i, r := range objects {
						if !bytes.Equal(payload[i][16:], c.bytes(r)[16:]) {
							t.Fatal("retry payload")
						}
					}
					if err := c.Verify(all); err != nil {
						t.Fatal(err)
					}
					live := map[Ref]uint32{}
					for _, r := range all {
						live[r] = c.entry(r).size
					}
					stage4Oracle(t, c, live)
				})
			}
		}
	}
}
