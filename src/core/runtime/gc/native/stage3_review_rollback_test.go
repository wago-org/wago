//go:build wagodebug

package gc

import "testing"

func TestStage3ReviewLatePromotionRollback(t *testing.T) {
	for _, point := range []failurePoint{failPromotionDestination, failPromotionCommit} {
		c := regionCollector(t)
		a := regionStruct(t, c, 0, EmptyRoots{})
		roots := Slots{(*Root)(&a)}
		entry := *c.entry(a)
		bump := c.nurseryBump
		cleanup := armFailure(c, point, 0)
		err := c.CollectMinor(roots)
		cleanup()
		if err == nil {
			t.Fatal("failure point did not fire")
		}
		if bump != c.nurseryBump || entry != *c.entry(a) {
			t.Fatal("failed transaction changed live Eden state")
		}
		if err := c.Verify(roots); err != nil {
			t.Fatal(err)
		}
		if err := c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
	}
}
