//go:build wagodebug

package gc

import (
	"slices"
	"testing"
)

func TestStage3ReviewLatePromotionRollback(t *testing.T) {
	for _, point := range []failurePoint{failPromotionDestination, failPromotionCommit} {
		c := regionCollector(t)
		a := regionStruct(t, c, 0, EmptyRoots{})
		roots := Slots{(*Root)(&a)}
		regions := slices.Clone(c.edenRegions)
		entry := *c.entry(a)
		cleanup := armFailure(c, point, 0)
		err := c.CollectMinor(roots)
		cleanup()
		if err == nil {
			t.Fatal("failure point did not fire")
		}
		if !slices.Equal(regions, c.edenRegions) || entry != *c.entry(a) {
			t.Fatal("failed transaction changed live region")
		}
		if err := c.Verify(roots); err != nil {
			t.Fatal(err)
		}
		if err := c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
	}
}
