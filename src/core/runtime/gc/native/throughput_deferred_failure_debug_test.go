//go:build wagodebug

package gc

import (
	"errors"
	"slices"
	"testing"
)

func TestInjectedThroughputReconciliationFailureRetainsPendingDebt(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 32}); err != nil {
		t.Fatal(err)
	}
	deferred, err := h.alloc(32, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.alloc(32, spaceLarge); err != nil {
		t.Fatal(err)
	}
	if err := h.deferFree(deferred); err != nil {
		t.Fatal(err)
	}
	beforePending := slices.Clone(h.pendingFree)
	beforeBytes := h.pendingBytes
	beforeFree := h.freeSpans()
	beforeBump := h.bump

	cleanup := armFailure(&h, failThroughputReconciliation, 0)
	_, err = h.alloc(32, spaceLarge)
	cleanup()
	if !errors.Is(err, errInjectedFailure) {
		t.Fatalf("allocation error=%v, want injected reconciliation failure", err)
	}
	if !slices.Equal(h.pendingFree, beforePending) || h.pendingBytes != beforeBytes ||
		!slices.Equal(h.freeSpans(), beforeFree) || h.bump != beforeBump {
		t.Fatalf("failed reconciliation lost debt: pending=%v/%d free=%v bump=%d", h.pendingFree, h.pendingBytes, h.freeSpans(), h.bump)
	}

	got, err := h.alloc(32, spaceLarge)
	if err != nil {
		t.Fatalf("retry allocation: %v", err)
	}
	if got.off != deferred.off || len(h.pendingFree) != 0 || h.pendingBytes != 0 {
		t.Fatalf("retry allocation/debt=%+v pending=%v/%d", got, h.pendingFree, h.pendingBytes)
	}
}
