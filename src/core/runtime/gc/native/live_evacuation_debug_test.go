//go:build wagodebug

package gc

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

func TestLiveOnlyEvacuationRollbackAndRetry(t *testing.T) {
	for workers := 2; workers <= 8; workers++ {
		t.Run(fmt.Sprintf("workers%d", workers), func(t *testing.T) {
			c, candidates := stage7Heap(t, workers, 385, 6, true)
			h := &c.throughput
			h.mature.pressure = true
			s, err := c.selectMatureEvacuation()
			if err != nil {
				t.Fatal(err)
			}
			roots := make(RefSliceRoots, 0, 1+len(candidates)/97)
			var liveDemand uint64
			var liveLargest uint32
			payload := make(map[Ref][]byte)
			for i, r := range candidates {
				if i%97 != 0 {
					continue
				}
				n := h.handleAllocSize(*c.entry(r))
				liveDemand += uint64(n)
				liveLargest = max(liveLargest, n)
				roots = append(roots, r)
				payload[r] = bytes.Clone(c.bytes(r))
			}
			h.limit = liveEvacuationAdmissionLimit(t, c, s, liveDemand, liveLargest)
			beforeHandles := append([]handleEntry(nil), c.handles...)
			beforeFree := h.freeSpans()
			beforeBump := h.bump
			h.mature.pressure = true
			disarm := armFailure(c, failEvacuationCommit, 0)
			err = c.CollectFull(roots)
			disarm()
			if err == nil {
				t.Fatal("commit failure was not reached")
			}
			if !reflect.DeepEqual(beforeHandles, c.handles) || !reflect.DeepEqual(beforeFree, h.freeSpans()) || beforeBump != h.bump {
				t.Fatal("failed live-only evacuation published or retained destination ownership")
			}
			for _, r := range roots {
				if !bytes.Equal(payload[r], c.bytes(r)) {
					t.Fatalf("failed evacuation changed source payload for %v", r)
				}
			}
			if err = c.Verify(candidates); err != nil {
				t.Fatal("rollback", err)
			}
			h.mature.pressure = true
			if err = c.CollectFull(roots); err != nil {
				t.Fatal("retry", err)
			}
			if d := c.CollectionDiagnostics(); d.Evacuation != "moved" || d.Demand != liveDemand || d.MovedPhysical != liveDemand {
				t.Fatalf("retry did not publish only live objects: %+v", d)
			}
			for _, r := range roots {
				if !bytes.Equal(payload[r], c.bytes(r)) {
					t.Fatalf("retry changed payload for %v", r)
				}
			}
			if err = c.Verify(roots); err != nil {
				t.Fatal("retry", err)
			}
		})
	}
}
