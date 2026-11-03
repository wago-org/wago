//go:build wagodebug

package gc

import "testing"

func TestConfiguredMatureRegionsFailedCollectionCancelsNativeReservation(t *testing.T) {
	c := regionCollector(t)
	live := regionStruct(t, c, 0, EmptyRoots{})
	roots := Slots{(*Root)(&live)}
	before := *c.entry(live)
	if !c.PrepareNativeAllocation(32) {
		t.Fatal("reservation refused")
	}
	epoch := c.nativeAllocEpoch
	cleanup := armFailure(c, failPromotionCommit, 0)
	err := c.CollectMinor(roots)
	cleanup()
	if err == nil {
		t.Fatal("missing transaction failure")
	}
	if c.nativeAllocEpoch == epoch || c.nativeStructAlloc.Count != 0 || c.nurseryBump != 32 || *c.entry(live) != before {
		t.Fatal("failure retained reservation or changed live object")
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}
