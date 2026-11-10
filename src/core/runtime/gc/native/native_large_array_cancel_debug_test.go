//go:build wagodebug

package gc

import (
	"errors"
	"slices"
	"testing"
)

func TestNativeLargeArrayCancellationBeforeFailedCollection(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, ThroughputHeapBytes: 8192, ThroughputPageBytes: 4096})
	first, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if !c.PrepareNativeArrayAllocationImmediate(512) {
		t.Fatal("large reservation unavailable")
	}
	reserved := c.nativeLargeArrayAlloc.Handle
	second, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	firstRoot, secondRoot := Root(first), Root(second)
	bump, epoch := c.nurseryBump, c.nativeLargeArrayEpoch
	cleanup := armFailure(c, failPromotionPlan, 0)
	err = c.CollectMinor(Slots{&firstRoot, &secondRoot})
	cleanup()
	if !errors.Is(err, errInjectedFailure) {
		t.Fatalf("collection error = %v, want injected failure", err)
	}
	if !slices.Equal(c.nurseryHandles, []uint32{handleOf(first), handleOf(second)}) || !slices.Equal(c.freeHandles, []uint32{reserved}) {
		t.Fatalf("failed collection lost or duplicated identities: nursery %v free %v", c.nurseryHandles, c.freeHandles)
	}
	if c.nurseryBump != bump || c.nativeLargeArrayEpoch == epoch || c.nativeLargeArrayAlloc.Count != 0 || c.nativeLargeArrayAlloc.Handle != 0 {
		t.Fatalf("failed collection retained a reservation or lost backing: state %+v bump %d/%d epoch %d/%d", c.nativeLargeArrayAlloc, c.nurseryBump, bump, c.nativeLargeArrayEpoch, epoch)
	}
	if firstRoot.GetRef() != first || secondRoot.GetRef() != second {
		t.Fatal("failed collection changed live roots")
	}
	if err := c.Verify(Slots{&firstRoot, &secondRoot}); err != nil {
		t.Fatal(err)
	}
}
