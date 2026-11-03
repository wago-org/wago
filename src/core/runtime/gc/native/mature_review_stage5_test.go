package gc

import (
	"math/rand"
	"sort"
	"testing"
)

type matureReviewExtent struct {
	e    handleEntry
	size uint32
}

// Physical extents are supplied by the fixture/model, never handleAllocSize.
// Require exact coverage by live objects + indexed/pending holes, and independently
// intersect those known extents with descriptor pages.
func matureReviewOracle(t *testing.T, h *throughputHeap, live []matureReviewExtent) {
	t.Helper()
	type span struct{ off, size uint32 }
	var all []span
	want := make([]uint32, len(h.mature.regions))
	for _, x := range live {
		all = append(all, span{x.e.off, x.size})
		for p := x.e.off; p < x.e.off+x.size; {
			i := p / h.pageBytes
			end := min(x.e.off+x.size, (i+1)*h.pageBytes)
			want[i] += end - p
			p = end
		}
	}
	for _, x := range h.freeSpans() {
		all = append(all, span(x))
	}
	for _, x := range h.pendingFree {
		all = append(all, span(x))
	}
	sort.Slice(all, func(i, j int) bool { return all[i].off < all[j].off })
	var end uint32
	for _, x := range all {
		if x.off != end || x.size == 0 {
			t.Fatalf("gap/overlap at %d: %+v", end, x)
		}
		end += x.size
	}
	if end != h.bump {
		t.Fatalf("coverage %d bump %d", end, h.bump)
	}
	for i, r := range h.mature.regions {
		if r.live != want[i] || r.reserved != 0 {
			t.Fatalf("page %d: %+v want %d", i, r, want[i])
		}
	}
	if h.mature.admission || uint64(cap(h.mature.regions)) > (uint64(h.limit)+uint64(h.pageBytes)-1)/uint64(h.pageBytes) {
		t.Fatal("admission/metadata bound")
	}
}

func TestMatureReviewStage5ExtentTransactions(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 65552, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096, NurseryRegionShift: 6}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	rng := rand.New(rand.NewSource(5141))
	var live []matureReviewExtent
	sizes := []uint32{16, 32, 48, 65, 129, 3000, 4080, 4097, 8208}
	classes := []uint32{32, 32, 48, 96, 192, 3072, 4096, 4112, 8208}
	for round := 0; round < 600; round++ {
		if len(live) > 0 && rng.Intn(3) == 0 {
			i := rng.Intn(len(live))
			if err := h.free(live[i].e); err != nil {
				t.Fatal(err)
			}
			live = append(live[:i], live[i+1:]...)
		}
		tx := h.beginAllocTransaction()
		var added []matureReviewExtent
		abort := round%3 == 0
		for n := 0; n < 1+rng.Intn(6); n++ {
			k := rng.Intn(len(sizes))
			sp := spaceOld
			base := classes[k]
			if rng.Intn(2) == 0 {
				sp = spaceLarge
				base = (sizes[k] + 15) &^ 15
			}
			// Derive first fit and absorbed tail from fixture-owned occupied intervals.
			occupied := append(append([]matureReviewExtent{}, live...), added...)
			sort.Slice(occupied, func(i, j int) bool { return occupied[i].e.off < occupied[j].e.off })
			off, physical := h.bump, base
			var cursor uint32
			for _, x := range occupied {
				if x.e.off-cursor >= base {
					off = cursor
					if x.e.off-cursor-base < 32 {
						physical = x.e.off - cursor
					}
					break
				}
				cursor = x.e.off + x.size
			}
			e, err := h.alloc(sizes[k], sp)
			if err != nil {
				abort = true
				break
			}
			if e.off != off {
				t.Fatalf("round %d first fit %d want %d", round, e.off, off)
			}
			added = append(added, matureReviewExtent{e, physical})
		}
		if abort {
			for i := len(added) - 1; i >= 0; i-- {
				h.rollbackSuccessfulAlloc(added[i].e, tx.bump)
			}
			h.restoreAllocTransaction(tx)
		} else {
			h.commitMatureAdmission()
			live = append(live, added...)
		}
		matureReviewOracle(t, &h, live)
	}
	for _, x := range live {
		if err := h.deferFree(x.e); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.reclaimEmptyMature(); err != nil {
		t.Fatal(err)
	}
	matureReviewOracle(t, &h, nil)
	if h.bump != 0 {
		t.Fatal("unreclaimed tail")
	}
}

func TestMatureReviewStage5MixedNativeCheckedCycles(t *testing.T) {
	c := stage4Collector(t, 6)
	for cycle := 0; cycle < 80; cycle++ {
		parent := regionStruct(t, c, 1, EmptyRoots{})
		roots := Slots{(*Root)(&parent)}
		if err := c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
		if !c.PrepareNativeAllocation(32) {
			t.Fatal("native reservation")
		}
		s := &c.nativeStructAlloc
		h := s.Handles[0]
		child := makeObjRef(h)
		c.handles[h] = handleEntry{off: s.ChunkCursor, size: 32, space: spaceNursery}
		c.writeHeader(child, ObjHeader{TypeID: 0, Size: 32, Flags: FlagPointerFree})
		s.ChunkCursor += 32
		s.Cursor++
		c.stats.Allocations++
		if err := c.StructSet(child, 1, I64Value(int64(cycle))); err != nil {
			t.Fatal(err)
		}
		if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
			t.Fatal(err)
		}
		epoch := c.nativeAllocEpoch
		los, err := c.NewArrayDefaultWithRoots(2, 2048, roots)
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, (*Root)(&los))
		if c.nativeStructAlloc.Count != 0 || c.nativeAllocEpoch == epoch {
			t.Fatal("checked LOS did not invalidate batch")
		}
		if err = c.ArraySet(los, 2047, I32Value(int32(cycle))); err != nil {
			t.Fatal(err)
		}
		original := *c.entry(los)
		for _, full := range []bool{true, false, true, false} {
			if full {
				err = c.CollectFull(roots)
			} else {
				err = c.CollectMinor(roots)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if c.entry(los).off != original.off {
			t.Fatal("LOS moved")
		}
		v, err := c.StructGet(child, 1)
		if err != nil || v.I64() != int64(cycle) {
			t.Fatal("remembered native child", err)
		}
		a, err := c.ArrayGet(los, 2047)
		if err != nil || a.I32() != int32(cycle) {
			t.Fatal("LOS payload", err)
		}
		matureReviewOracle(t, &c.throughput, []matureReviewExtent{{*c.entry(parent), 32}, {*c.entry(child), 32}, {*c.entry(los), 8208}})
		if err = c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		matureReviewOracle(t, &c.throughput, nil)
		if c.throughput.bump != 0 {
			t.Fatal("retirement leak")
		}
		bump := c.throughput.bump
		if err = c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		if c.throughput.bump != bump {
			t.Fatal("duplicate reclamation changed high-water mark")
		}
	}
}
