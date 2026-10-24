package gc

import (
	"fmt"
	"slices"
	"testing"
)

func TestNativeBatchCancellationPreservesActiveArrayReservation(t *testing.T) {
	for _, checked := range []bool{false, true} {
		for _, bytes := range []uint32{512, 40 << 10} {
			t.Run(fmt.Sprintf("checked=%t/bytes=%d", checked, bytes), func(t *testing.T) {
				c := batchTestCollector(t)
				if checked {
					if err := c.EnableCheckedHandles(); err != nil {
						t.Fatal(err)
					}
				}
				// The later independent array reservation makes the struct batch cease
				// to be the nursery ledger's tail, exercising cancellation's fallback.
				if !c.PrepareNativeStructAllocation(16) {
					t.Fatal("struct reservation unavailable")
				}
				if !c.PrepareNativeArrayAllocationImmediate(bytes) {
					t.Fatal("array reservation unavailable")
				}
				reserved := c.nativeLargeArrayAlloc
				c.CancelNativeAllocationBatch()
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
