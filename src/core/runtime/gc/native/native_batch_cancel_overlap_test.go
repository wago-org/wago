package gc

import (
	"fmt"
	"slices"
	"testing"
)

func TestNativeBatchCancellationPreservesActiveArrayReservation(t *testing.T) {
	for _, checked := range []bool{false, true} {
		for _, bytes := range []uint32{512, 40 << 10} {
			for _, chunk := range []bool{false, true} {
				t.Run(fmt.Sprintf("checked=%t/bytes=%d/chunk=%t", checked, bytes, chunk), func(t *testing.T) {
					c := batchTestCollector(t)
					if checked {
						if err := c.EnableCheckedHandles(); err != nil {
							t.Fatal(err)
						}
					}
					// The later independent array reservation makes the first batch cease
					// to be the nursery ledger's tail, exercising cancellation's fallback.
					prepare := c.PrepareNativeStructAllocation
					if chunk {
						prepare = c.PrepareNativeAllocation
					}
					if !prepare(16) {
						t.Fatal("batch reservation unavailable")
					}
					if !c.PrepareNativeArrayAllocationImmediate(bytes) {
						t.Fatal("array reservation unavailable")
					}
					reserved := c.nativeLargeArrayAlloc
					bump := c.nurseryBump
					c.CancelNativeAllocationBatch()
					if spaceKind((reserved.ChunkBump>>16)&0xff) == spaceNursery && c.nurseryBump != bump {
						t.Fatalf("cancellation moved bump from %d to %d", bump, c.nurseryBump)
					}
					if !slices.Equal(c.nurseryHandles, []uint32{reserved.Handle}) {
						t.Fatalf("ledger after cancellation = %v", c.nurseryHandles)
					}
					// Reproduce the reported [0,512) versus [0,16) overlap through an ordinary
					// allocation while the unpublished native array reservation remains live.
					ref, err := c.NewStructDefault(0)
					if err != nil {
						t.Fatal(err)
					}
					e := c.handles[handleOf(ref)]
					if spaceKind((reserved.ChunkBump>>16)&0xff) == spaceNursery &&
						e.off < reserved.ChunkEnd && reserved.ChunkStart < e.off+e.size {
						t.Fatalf("allocation [%d,%d) overlaps active reservation [%d,%d)", e.off, e.off+e.size, reserved.ChunkStart, reserved.ChunkEnd)
					}
					if c.nativeLargeArrayAlloc != reserved {
						t.Fatal("cancellation changed independent reservation")
					}
					if !slices.Contains(c.nurseryHandles, reserved.Handle) {
						t.Fatal("cancellation removed active reservation from nursery ledger")
					}
					if slices.Contains(c.freeHandles, reserved.Handle) {
						t.Fatal("active reservation handle recycled")
					}
					c.discardNativeLargeArrayAllocation()
					if slices.Contains(c.nurseryHandles, reserved.Handle) {
						t.Fatal("canceled array remains in ledger")
					}
				})
			}
		}
	}
}

func TestNativeBatchCancellationReclaimsPromotedNurseryWithoutReservation(t *testing.T) {
	c := batchTestCollector(t)
	if !c.PrepareNativeStructHandles() {
		t.Fatal("batch reservation unavailable")
	}
	publishBatchTestStructs(c, 1)
	ref := makeObjRef(c.nativeStructAlloc.Handles[0])
	if err := c.ForcePromote(ref); err != nil {
		t.Fatal(err)
	}
	// Promotion removes the consumed identity from the nursery ledger, so the
	// remaining reservation is no longer an exact tail batch. No array or dead
	// allocation charge owns the now-empty nursery extent.
	if c.hasPendingDeadNurseryObjects() {
		t.Fatal("unexpected pending dead allocation charge")
	}
	c.CancelNativeAllocationBatch()
	if c.nurseryBump != 0 || len(c.nurseryHandles) != 0 {
		t.Fatalf("cancellation retained empty nursery: bump=%d handles=%v", c.nurseryBump, c.nurseryHandles)
	}
	if !c.validObjectRef(ref) {
		t.Fatal("cancellation invalidated promoted object")
	}
}
