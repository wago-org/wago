package gc

import (
	"bytes"
	"fmt"
	"testing"
)

// All reservations use the real allocator. Small holes
// are separated by valid old array objects; there are no invented free lists.
func newAsymmetricRunCollector(t *testing.T, workers int, optional bool) (*Collector, RefSliceRoots, RefSliceRoots, matureSelection) {
	t.Helper()
	c := newTestCollector(t, Config{Workers: workers, NurseryBytes: 8 << 20, NurseryRegionShift: 16, MatureEvacuation: true, ThroughputHeapBytes: 16 << 20, ThroughputClassLimit: 8192, ThroughputPageBytes: 4096})
	h := &c.throughput
	var holes []handleEntry
	var all RefSliceRoots
	for group := 0; group < workers+8; group++ {
		for lane := 0; lane < workers; lane++ {
			n := uint32(32768)
			if lane == 0 {
				n = 128
			}
			e, err := h.alloc(n, spaceOld)
			if err != nil {
				t.Fatal(err)
			}
			holes = append(holes, e)
			if lane == 0 {
				r, err := c.NewArrayDefault(2, (32768-128-16)/4)
				if err != nil {
					t.Fatal(err)
				}
				if c.entry(r).size != 32640 {
					t.Fatalf("array size %d", c.entry(r).size)
				}
				dst, err := h.alloc(32640, spaceOld)
				if err != nil {
					t.Fatal(err)
				}
				c.promoteHandleTo(handleOf(r), dst)
				c.removeYoungHandle(handleOf(r))
				all = append(all, r)
			}
		}
	}
	var objects RefSliceRoots
	var s matureSelection
	for i := 0; i < 2048; i++ {
		var r Ref
		var err error
		if i%11 == 0 {
			r, err = c.NewArrayDefault(2, 8)
		} else {
			r, err = c.NewStructDefault(0)
		}
		if err != nil {
			t.Fatal(err)
		}
		// Payload beyond the header carries identity through the real copy path.
		for k := 16; k < len(c.bytes(r)); k++ {
			c.bytes(r)[k] = byte(i + k)
		}
		if optional {
			if err = c.promoteHandle(handleOf(r)); err != nil {
				t.Fatal(err)
			}
		}
		objects = append(objects, r)
		all = append(all, r)
		if optional && (i+1)%128 == 0 {
			gap, err := h.alloc(32768-h.bump%32768, spaceOld)
			if err != nil {
				t.Fatal(err)
			}
			holes = append(holes, gap)
		}
	}
	for _, e := range holes {
		if err := h.free(e); err != nil {
			t.Fatal(err)
		}
	}
	if optional {
		h.mature.pressure = true
		var err error
		s, err = c.selectMatureEvacuation()
		if err != nil {
			t.Fatal(err)
		}
		if len(s.moves) != len(objects) || s.demand != 68528 {
			t.Fatalf("selection moves=%d demand=%d", len(s.moves), s.demand)
		}
	}
	c.clearMarks()
	for _, r := range all {
		c.mark[handleOf(r)] = true
	}
	c.markHighWater = uint32(len(c.mark))
	if err := c.Verify(all); err != nil {
		t.Fatal("fixture", err)
	}
	return c, all, objects, s
}

func collectWithAdmittedRuns(c *Collector, objects RefSliceRoots, s matureSelection, optional bool) error {
	if optional {
		return c.evacuateMature(s)
	}
	plans := make([]plannedPromotion, len(objects))
	for i, r := range objects {
		plans[i].handle = handleOf(r)
	}
	_, _, _, err := c.promoteWithRuns(plans, 0, 0)
	return err
}
func TestRunClaimsPreserveObjectsWithAsymmetricCapacity(t *testing.T) {
	for workers := 2; workers <= 8; workers++ {
		for _, optional := range []bool{false, true} {
			t.Run(fmt.Sprintf("workers%d/optional%t", workers, optional), func(t *testing.T) {
				c, all, objects, s := newAsymmetricRunCollector(t, workers, optional)
				before := append([]handleEntry(nil), c.handles...)
				payload := make([][]byte, len(objects))
				for i, r := range objects {
					payload[i] = bytes.Clone(c.bytes(r))
				}
				if err := collectWithAdmittedRuns(c, objects, s, optional); err != nil {
					t.Fatal("production path", err)
				}
				if optional && c.CollectionDiagnostics().Evacuation != "moved" {
					t.Fatalf("production path: %+v", c.CollectionDiagnostics())
				}
				for i, r := range objects {
					if c.entry(r).off == before[handleOf(r)].off && optional {
						t.Fatal("not moved")
					}
					if !bytes.Equal(payload[i][16:], c.bytes(r)[16:]) {
						t.Fatal("payload/identity changed")
					}
					if c.entry(r).space != spaceOld {
						t.Fatal("not old")
					}
				}
				if err := c.Verify(all); err != nil {
					t.Fatal("post-copy", err)
				}
				// Include allocator tails, pending retired sources, fillers, and every live
				// physical object in an independent exact-coverage oracle.
				live := map[Ref]uint32{}
				for _, r := range all {
					live[r] = c.entry(r).size
				}
				stage4Oracle(t, c, live)
				tinyTail := false
				for _, span := range c.throughput.freeSpans() {
					if span.size == 16 {
						tinyTail = true
					}
				}
				if !tinyTail {
					t.Fatal("fixture did not exercise abandoned 16-byte tail")
				}
				// Releasing all identities checks ownership and coalescing of the run tails.
				for _, r := range all {
					c.releaseHandle(handleOf(r), true, false)
				}
				if err := c.throughput.sweepAllPending(); err != nil {
					t.Fatal(err)
				}
				if err := c.Verify(nil); err != nil {
					t.Fatal("release", err)
				}
			})
		}
	}
}
