package gc

import (
	"reflect"
	"testing"
	"unsafe"
)

func assertMatureAdmissionClean(t *testing.T, h *throughputHeap) {
	t.Helper()
	if h.mature.admission || len(h.mature.touched) != 0 {
		t.Fatalf("mature admission retained state: admission=%v touched=%v", h.mature.admission, h.mature.touched)
	}
	for i, r := range h.mature.regions {
		if r.reserved != 0 || r.admissionTouched {
			t.Fatalf("mature region %d retained transaction state: %+v", i, r)
		}
	}
}

func TestMatureAdmissionTouchedIndexesDeduplicateAfterRollback(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 4 << 12, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096, NurseryRegionShift: 6}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	seed, err := h.alloc(4096, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.free(seed); err != nil {
		t.Fatal(err)
	}

	h.beginMatureAdmission(true)
	first, err := h.alloc(4096, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.mature.touched) != 1 {
		t.Fatalf("first reservation touched %v", h.mature.touched)
	}
	retainedCap := cap(h.mature.touched)
	for range 32 {
		h.releaseMature(first)
		if h.mature.regions[0].reserved != 0 {
			t.Fatal("rollback retained reservation")
		}
		if err := h.prepareMature(first); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.mature.touched) != 1 || cap(h.mature.touched) != retainedCap {
		t.Fatalf("retouch indexes/capacity = %v/%d, want [0]/%d", h.mature.touched, cap(h.mature.touched), retainedCap)
	}
	h.commitMatureAdmission()
	if h.mature.regions[0].live != 4096 {
		t.Fatalf("committed occupancy = %+v", h.mature.regions[0])
	}
	assertMatureAdmissionClean(t, &h)
}

func TestMatureAdmissionTouchedIndexesSpanPagesAndReset(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 8 << 12, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096, NurseryRegionShift: 6}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	tx := h.beginAllocTransaction()
	a, err := h.alloc(4080, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.alloc(4096, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.mature.touched; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("page-spanning admission touched %v", got)
	}
	h.rollbackSuccessfulAlloc(b, tx.bump)
	h.rollbackSuccessfulAlloc(a, tx.bump)
	h.restoreAllocTransaction(tx)
	assertMatureAdmissionClean(t, &h)

	tx = h.beginAllocTransaction()
	c, err := h.alloc(4096, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.mature.touched) != 1 || h.mature.touched[0] != 0 {
		t.Fatalf("retry retained stale indexes: %v", h.mature.touched)
	}
	h.rollbackSuccessfulAlloc(c, tx.bump)
	h.restoreAllocTransaction(tx)
	assertMatureAdmissionClean(t, &h)
}

func TestMatureAdmissionBookkeepingStorage(t *testing.T) {
	// Each page intersection now updates only authoritative live/reserved state
	// plus the two admission/reclamation flags. LOS mirrors, epochs, and global
	// refusal/retirement counters had no production consumer.
	if typ := reflect.TypeFor[matureRegion](); typ.NumField() != 4 || unsafe.Sizeof(matureRegion{}) != 12 {
		t.Fatalf("mature descriptor storage = %d fields/%d bytes, want 4/12", typ.NumField(), unsafe.Sizeof(matureRegion{}))
	}
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if h.mature != nil {
		t.Fatal("region-off heap allocated mature admission storage")
	}
}
