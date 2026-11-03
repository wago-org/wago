//go:build wagodebug

package gc

import (
	"errors"
	"testing"
)

// The removed page ledger no longer has a private metadata-allocation failure.
// Preserve failure ordering at the surviving admission boundary: backing must
// fail before allocator publication, and the exact same admission must retry.
func TestMatureAdmissionBackingFailureAndRetry(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 16 << 10, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096, NurseryRegionShift: 6}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.beginMatureAdmission(true)
	cleanup := armFailure(&h, failBackingGrowth, 0)
	_, err := h.alloc(4096, spaceOld)
	cleanup()
	if !errors.Is(err, errInjectedFailure) {
		t.Fatalf("backing admission failure = %v", err)
	}
	if h.bump != 0 || len(h.mem) != 0 || h.freeBytes != 0 || h.pendingBytes != 0 {
		t.Fatal("failed admission published allocator state")
	}
	h.cancelMatureAdmission()
	h.beginMatureAdmission(true)
	e, err := h.alloc(4096, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	h.commitMatureAdmission()
	if e.off != 0 || h.bump != 4096 {
		t.Fatalf("retry extent=%+v bump=%d", e, h.bump)
	}
}
