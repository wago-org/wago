//go:build wagodebug

package gc

import "testing"

func TestContiguousEdenCheckedPublicationCancelsBatch(t *testing.T) {
	for _, array := range []bool{false, true} {
		c := regionCollector(t)
		live := regionStruct(t, c, 0, EmptyRoots{})
		roots := Slots{(*Root)(&live)}
		before := *c.entry(live)
		bump := c.nurseryBump
		if array {
			if !c.PrepareNativeAllocation(32) {
				t.Fatal("reserve")
			}
		} else {
			if !c.PrepareNativeStructAllocation(32) {
				t.Fatal("reserve")
			}
		}
		epoch := c.nativeAllocEpoch
		clean := armFailure(c, failHandlePublication, 0)
		_, e := c.NewStructDefaultWithRoots(0, roots)
		clean()
		if !isInjectedFailure(e) || c.nativeStructAlloc.Count != 0 || c.nativeAllocEpoch == epoch || c.nurseryBump != bump || *c.entry(live) != before {
			t.Fatalf("rollback %v bump%d", e, c.nurseryBump)
		}
		if e := c.CollectMinor(roots); e != nil {
			t.Fatal(e)
		}
	}
}

func TestContiguousEdenNonCapacityMinorErrorWins(t *testing.T) {
	c := regionCollector(t)
	var live Ref
	for i := 0; i < 4; i++ {
		live = regionStruct(t, c, 0, EmptyRoots{})
	}
	roots := Slots{(*Root)(&live)}
	before := *c.entry(live)
	clean := armFailure(c, failPromotionPlan, 0)
	_, e := c.NewStructDefaultWithRoots(0, roots)
	clean()
	if !isInjectedFailure(e) || c.stats.FullCollections != 0 || *c.entry(live) != before {
		t.Fatalf("first error replaced %v", e)
	}
}
