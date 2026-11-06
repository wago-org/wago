package gc

import (
	"reflect"
	"testing"
)

func assertMatureAdmissionClean(t *testing.T, h *throughputHeap) {
	t.Helper()
	if h.mature.admission {
		t.Fatal("mature admission retained unpublished state")
	}
}

func TestMatureAdmissionUsesExactAllocatorTransaction(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 8 << 12, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096, NurseryRegionShift: 6}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	beforeBump, beforeMem := h.bump, len(h.mem)
	tx := h.beginAllocTransaction()
	a, err := h.alloc(4080, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.alloc(4096, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	h.rollbackSuccessfulAlloc(b, tx.bump)
	h.rollbackSuccessfulAlloc(a, tx.bump)
	h.restoreAllocTransaction(tx)
	assertMatureAdmissionClean(t, &h)
	if h.bump != beforeBump || len(h.mem) != beforeMem || h.freeBytes != 0 || h.pendingBytes != 0 {
		t.Fatal("allocator transaction did not restore exact state")
	}
}

func TestMaturePerPageLedgerStorageRemoved(t *testing.T) {
	typ := reflect.TypeFor[matureHeap]()
	for _, name := range []string{"regions", "touched"} {
		if _, ok := typ.FieldByName(name); ok {
			t.Fatalf("per-page ledger field %q remains", name)
		}
	}
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 1 << 30, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096, NurseryRegionShift: 6}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.alloc(32, spaceOld); err != nil {
		t.Fatal(err)
	}
	// A huge configured heap with one committed object has no page-sized
	// occupancy/touched allocation; only the exact allocator backing is present.
	if h.bump != 32 || len(h.mem) != 4096 {
		t.Fatalf("tiny committed heap = bump %d backing %d", h.bump, len(h.mem))
	}
}
