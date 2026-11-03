package gc

import (
	"fmt"
	"sort"
	"testing"
)

func liveEvacuationAdmissionLimit(t *testing.T, c *Collector, s matureSelection, liveDemand uint64, liveLargest uint32) uint32 {
	t.Helper()
	h := &c.throughput
	workers := max(1, c.cfg.Workers)
	for limit := (h.bump + 15) &^ 15; limit <= h.limit; limit += 16 {
		free := h.freeSpans()
		if h.bump < limit {
			free = append(free, throughputFreeSpan{h.bump, (limit - h.bump) &^ 15})
		}
		if _, _, _, err := planEvacuationRuns(free, s.excluded, liveDemand, liveLargest, workers, evacuationRunLimit); err != nil {
			continue
		}
		if _, _, _, err := planEvacuationRuns(free, s.excluded, s.demand, s.largest, workers, evacuationRunLimit); err != nil {
			return limit
		}
	}
	t.Fatal("fixture cannot separate live and pre-mark admission")
	return 0
}

func TestFullCollectionAdmitsOnlyLiveEvacuationDemand(t *testing.T) {
	for workers := 2; workers <= 8; workers++ {
		t.Run(fmt.Sprintf("workers%d", workers), func(t *testing.T) {
			c, candidates := stage7Heap(t, workers, 385, 6, true)
			h := &c.throughput
			h.mature.pressure = true
			s, err := c.selectMatureEvacuation()
			if err != nil {
				t.Fatal(err)
			}
			if len(s.moves) != len(candidates) {
				t.Fatalf("selected %d of %d candidate objects", len(s.moves), len(candidates))
			}

			roots := make(RefSliceRoots, 0, 1+len(candidates)/97)
			var liveDemand uint64
			var liveLargest uint32
			before := make(map[Ref]uint32)
			for i, r := range candidates {
				if i%97 != 0 {
					continue
				}
				n := h.handleAllocSize(*c.entry(r))
				liveDemand += uint64(n)
				liveLargest = max(liveLargest, n)
				roots = append(roots, r)
				before[r] = c.entry(r).off
			}
			h.limit = liveEvacuationAdmissionLimit(t, c, s, liveDemand, liveLargest)
			// selectMatureEvacuation rotates only the scan start; every eligible
			// component still fits this fixture's candidate budget.
			h.mature.pressure = true
			if err = c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			d := c.CollectionDiagnostics()
			if d.Evacuation != "moved" || d.Demand != liveDemand || d.MovedPhysical != liveDemand || d.Candidates != len(candidates) {
				t.Fatalf("live-only evacuation diagnostics: %+v, want demand/moved %d candidates %d", d, liveDemand, len(candidates))
			}
			for _, r := range roots {
				if c.entry(r).off == before[r] {
					t.Fatalf("live object %v was not evacuated", r)
				}
			}
			for i, r := range candidates {
				if i%97 != 0 && c.entry(r).space != spaceFree {
					t.Fatalf("dead candidate %v retained destination or source ownership", r)
				}
			}
			if err = c.Verify(roots); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFullCollectionAllDeadEvacuationNeedsNoDestination(t *testing.T) {
	c, candidates := stage7Heap(t, 8, 129, 6, true)
	h := &c.throughput
	h.limit = h.bump
	h.mature.pressure = true
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	d := c.CollectionDiagnostics()
	if d.Evacuation != "no-live" || d.Demand != 0 || d.Runs != 0 || d.MovedPhysical != 0 || d.Candidates != len(candidates) {
		t.Fatalf("all-dead evacuation reserved a destination: %+v", d)
	}
	for _, r := range candidates {
		if c.entry(r).space != spaceFree {
			t.Fatalf("dead candidate %v was not reclaimed", r)
		}
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
}

func TestFullCollectionPacksMixedLiveObjectsWithoutDeadSlots(t *testing.T) {
	c := newTestCollector(t, Config{Workers: 1, NurseryBytes: 1 << 20, NurseryRegionShift: 6, MatureEvacuation: true, ThroughputHeapBytes: 4 << 20, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	h := &c.throughput
	objects := make(RefSliceRoots, 0, 48)
	for i := 0; i < cap(objects); i++ {
		var (
			r   Ref
			err error
		)
		if i%3 == 0 {
			r, err = c.NewStructDefault(0)
		} else {
			r, err = c.NewArrayDefault(2, uint32(4+i%11))
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = c.ForcePromote(r); err != nil {
			t.Fatal(err)
		}
		objects = append(objects, r)
	}
	h.mature.pressure = true
	s, err := c.selectMatureEvacuation()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.moves) != len(objects) {
		t.Fatalf("selected %d of %d mixed objects", len(s.moves), len(objects))
	}
	roots := make(RefSliceRoots, 0, len(objects)/2)
	var liveDemand uint64
	var liveLargest uint32
	for i, r := range objects {
		if i%2 != 0 {
			continue
		}
		n := h.handleAllocSize(*c.entry(r))
		liveDemand += uint64(n)
		liveLargest = max(liveLargest, n)
		roots = append(roots, r)
	}
	h.limit = liveEvacuationAdmissionLimit(t, c, s, liveDemand, liveLargest)
	h.mature.pressure = true
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	sort.Slice(roots, func(i, j int) bool { return c.entry(roots[i]).off < c.entry(roots[j]).off })
	for i := 1; i < len(roots); i++ {
		previous := c.entry(roots[i-1])
		if want := previous.off + h.handleAllocSize(*previous); c.entry(roots[i]).off != want {
			t.Fatalf("dead candidate left destination gap after %v: got %d, want %d", roots[i-1], c.entry(roots[i]).off, want)
		}
	}
	if d := c.CollectionDiagnostics(); d.Demand != liveDemand || d.MovedPhysical != liveDemand {
		t.Fatalf("mixed live packing diagnostics: %+v", d)
	}
	if err = c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}
