package gc

import (
	"errors"
	"testing"
)

func stage5Occupancy(t *testing.T, h *throughputHeap, live, reserved uint64) {
	t.Helper()
	var l, r uint64
	for _, d := range h.mature.regions {
		l += uint64(d.live)
		r += uint64(d.reserved)
	}
	if l != live || r != reserved {
		t.Fatalf("descriptor totals live/reserved=%d/%d want %d/%d", l, r, live, reserved)
	}
	if uint64(cap(h.mature.regions)) > (uint64(h.limit)+uint64(h.pageBytes)-1)/uint64(h.pageBytes) {
		t.Fatal("unbounded metadata")
	}
	if !h.mature.admission {
		assertMatureAdmissionClean(t, h)
	}
}

func TestRFWGCMatureStage5AdmissionAndFragmentation(t *testing.T) {
	c := stage4Collector(t, 6)
	h := &c.throughput
	if len(h.mature.regions) != 0 {
		t.Fatal("metadata allocated before use")
	}
	var e [4]handleEntry
	for i := range e {
		var err error
		e[i], err = h.alloc(4096, spaceLarge)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, i := range []int{0, 2} {
		if err := h.free(e[i]); err != nil {
			t.Fatal(err)
		}
	}
	stage5Occupancy(t, h, 8192, 0)
	tx := h.beginAllocTransaction()
	if _, err := h.alloc(8192, spaceLarge); !errors.Is(err, errThroughputHeapExhausted) {
		t.Fatalf("fragmented extent admitted: %v", err)
	}
	h.restoreAllocTransaction(tx)
	if !h.mature.pressure {
		t.Fatal("required refusal did not request collection")
	}
	stage5Occupancy(t, h, 8192, 0)
	// Optional destination admission shares the transaction but cannot latch a
	// required refusal. There is deliberately no evacuation driver in this slice.
	tx = throughputAllocTransaction{mem: h.mem, bump: h.bump}
	h.beginMatureAdmission(false)
	if _, err := h.alloc(8192, spaceLarge); err == nil {
		t.Fatal("optional fragmented admission succeeded")
	}
	h.restoreAllocTransaction(tx)
	tx = h.beginAllocTransaction()
	a, err := h.alloc(4080, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.alloc(4096, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	stage5Occupancy(t, h, 8192, 8192) // class rounding, not payload bytes
	h.rollbackSuccessfulAlloc(b, tx.bump)
	h.rollbackSuccessfulAlloc(a, tx.bump)
	h.restoreAllocTransaction(tx)
	stage5Occupancy(t, h, 8192, 0)
	tx = h.beginAllocTransaction()
	a, err = h.alloc(4096, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if a.off != 0 {
		t.Fatal("cancel lost lowest-address free run")
	}
	h.commitMatureAdmission()
	stage5Occupancy(t, h, 12288, 0)
}

func TestRFWGCMatureStage5CrossBoundaryReclamation(t *testing.T) {
	c := stage4Collector(t, 6)
	small := regionStruct(t, c, 0, EmptyRoots{})
	roots := Slots{(*Root)(&small)}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	large, err := c.NewArrayDefaultWithRoots(2, 2048, roots) // 8208 physical bytes, starts at 32
	if err != nil {
		t.Fatal(err)
	}
	roots = append(roots, (*Root)(&large))
	if err := c.ArraySet(large, 2047, I32Value(42)); err != nil {
		t.Fatal(err)
	}
	original := *c.entry(large)
	if original.off != 32 || original.size != 8208 {
		t.Fatalf("fixture extent %+v", original)
	}
	h := &c.throughput
	stage5Occupancy(t, h, 8240, 0)
	for i, want := range []uint32{4096, 4096, 48} {
		if h.mature.regions[i].live != want {
			t.Fatalf("page %d %+v", i, h.mature.regions[i])
		}
	}
	small = 0
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if *c.entry(large) != original {
		t.Fatal("nonmoving LOS changed")
	}
	v, err := c.ArrayGet(large, 2047)
	if err != nil || v.I32() != 42 {
		t.Fatal("lost boundary payload")
	}
	stage5Occupancy(t, h, 8208, 0)
	large = 0
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	stage5Occupancy(t, h, 0, 0)
	if h.bump != 0 || len(h.pendingFree) != 0 {
		t.Fatalf("no descriptor-driven reclaim: bump=%d pending=%d", h.bump, len(h.pendingFree))
	}
	for _, r := range h.mature.regions {
		if r.retire {
			t.Fatal("retirement not committed", r)
		}
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
}

func TestRFWGCMatureStage5Churn(t *testing.T) {
	c := stage4Collector(t, 6)
	parent := regionStruct(t, c, 1, EmptyRoots{})
	roots := Slots{(*Root)(&parent)}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20001; i++ {
		child := regionStruct(t, c, 0, roots)
		if err := c.StructSet(child, 1, I64Value(int64(i))); err != nil {
			t.Fatal(err)
		}
		if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
			t.Fatal(err)
		}
		if err := c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
		if err := c.CollectFull(roots); err != nil {
			t.Fatal(err)
		}
		stage5Occupancy(t, &c.throughput, 64, 0)
		if c.throughput.mature.admission || len(c.throughput.mature.regions) != 1 || c.throughput.bump > 96 {
			t.Fatal("unbounded or unpublished churn")
		}
		v, err := c.StructGet(child, 1)
		if err != nil || v.I64() != int64(i) {
			t.Fatal("remembered child lost")
		}
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	stage5Occupancy(t, &c.throughput, 0, 0)
	if c.throughput.bump != 0 {
		t.Fatal("churn page not reclaimed")
	}
}

func TestRFWGCMatureStage5AbsorbedTail(t *testing.T) {
	c := stage4Collector(t, 6)
	h := &c.throughput
	e, err := h.alloc(48, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.alloc(32, spaceOld); err != nil {
		t.Fatal(err)
	}
	if err = h.free(e); err != nil {
		t.Fatal(err)
	}
	tx := h.beginAllocTransaction()
	e, err = h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if h.handleAllocSize(e) != 48 {
		t.Fatal("tail not absorbed")
	}
	stage5Occupancy(t, h, 32, 48)
	h.rollbackSuccessfulAlloc(e, tx.bump)
	h.restoreAllocTransaction(tx)
	stage5Occupancy(t, h, 32, 0)
}

func TestRFWGCMatureStage5DefaultOff(t *testing.T) {
	c := stage4Collector(t, 0)
	r := regionStruct(t, c, 0, EmptyRoots{})
	if err := c.ForcePromote(r); err != nil {
		t.Fatal(err)
	}
	if c.throughput.mature != nil {
		t.Fatal("default mature policy activated")
	}
}

func TestRFWGCMatureStage5ZeroPromotionCredit(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, NurseryRegionShift: 6, SurvivorBytes: 4096, ThroughputHeapBytes: 16384, ThroughputPageBytes: 4096})
	var large [2]Ref
	roots := Slots{(*Root)(&large[0]), (*Root)(&large[1])}
	for i := range large {
		var err error
		large[i], err = c.NewArrayDefaultWithRoots(2, 2044, roots)
		if err != nil {
			t.Fatal(err)
		}
	}
	child := regionStruct(t, c, 0, roots)
	roots = append(roots, (*Root)(&child))
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	stage5Occupancy(t, &c.throughput, 16384, 0)
	if c.entry(child).space != spaceNursery || c.throughput.mature.admission {
		t.Fatal("zero-credit survivor not retained")
	}
}

func TestRFWGCMatureStage5PartialFinalPage(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 8208, ThroughputPageBytes: 4096, ThroughputClassLimit: 32, NurseryRegionShift: 6}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	a, err := h.alloc(8192, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.alloc(16, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	stage5Occupancy(t, &h, 8208, 0)
	if len(h.mature.regions) != 3 || h.mature.regions[2].live != 16 || len(h.mem) != 8208 {
		t.Fatal("partial final page bound")
	}
	for _, size := range []uint32{16, ^uint32(0)} {
		tx := h.beginAllocTransaction()
		if _, err := h.alloc(size, spaceLarge); err == nil {
			t.Fatal("oversize admitted", size)
		}
		h.restoreAllocTransaction(tx)
		stage5Occupancy(t, &h, 8208, 0)
	}
	if err = h.free(b); err != nil {
		t.Fatal(err)
	}
	if err = h.free(a); err != nil {
		t.Fatal(err)
	}
	if err = h.reclaimEmptyMature(); err != nil {
		t.Fatal(err)
	}
	if h.bump != 0 {
		t.Fatal("partial page not reclaimed")
	}
}

func TestRFWGCMatureStage5RetirementPolicyControl(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		c := stage4Collector(t, shift)
		if _, err := c.NewArrayDefault(2, 2048); err != nil {
			t.Fatal(err)
		}
		if err := c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		if shift == 0 {
			if len(c.throughput.pendingFree) == 0 || c.throughput.bump == 0 {
				t.Fatal("baseline deferred-ledger control changed")
			}
		} else if len(c.throughput.pendingFree) != 0 || c.throughput.bump != 0 {
			t.Fatal("region policy did not drive reclamation")
		}
	}
}
