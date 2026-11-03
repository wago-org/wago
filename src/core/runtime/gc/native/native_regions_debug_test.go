//go:build wagodebug

package gc

import "testing"

func TestNativeRegionsFailedCollectionCancelsReservation(t *testing.T) {
	c := regionCollector(t)
	live := regionStruct(t, c, 0, EmptyRoots{})
	roots := Slots{(*Root)(&live)}
	before := *c.entry(live)
	if !c.PrepareNativeStructAllocation(32) {
		t.Fatal("reservation refused")
	}
	epoch := c.nativeAllocEpoch
	cleanup := armFailure(c, failPromotionCommit, 0)
	err := c.CollectMinor(roots)
	cleanup()
	if err == nil {
		t.Fatal("missing transaction failure")
	}
	if c.nativeAllocEpoch == epoch || c.nativeStructAlloc.Count != 0 || c.edenRegions[0].bump != 32 || *c.entry(live) != before {
		t.Fatal("failure retained reservation or changed live object")
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
	if !c.PrepareNativeStructAllocation(32) {
		t.Fatal("retry reservation refused")
	}
	cleanup = armFailure(c, failHandlePublication, 0)
	_, err = c.NewStructDefaultWithRoots(0, roots)
	cleanup()
	if err == nil || c.edenRegions[0].bump != 32 || c.nativeStructAlloc.Count != 0 {
		t.Fatal("checked publication rollback retained reservation")
	}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
}
