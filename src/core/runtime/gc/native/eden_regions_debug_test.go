//go:build wagodebug

package gc

import (
	"slices"
	"testing"
)

func TestEdenRegionsPublicationRollback(t *testing.T) {
	c := regionCollector(t)
	regionStruct(t, c, 0, EmptyRoots{})
	before := slices.Clone(c.edenRegions)
	bump := c.nurseryBump
	stats := c.Stats()
	cleanup := armFailure(c, failHandlePublication, 0)
	_, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
	cleanup()
	if err == nil {
		t.Fatal("missing injected failure")
	}
	if !slices.Equal(before, c.edenRegions) || bump != c.nurseryBump || stats != c.Stats() {
		t.Fatal("publication failure consumed region")
	}
	regionStruct(t, c, 0, EmptyRoots{})
}

func TestEdenRegionsFailedEvacuationPreservesRegions(t *testing.T) {
	c := regionCollector(t)
	a := regionStruct(t, c, 0, EmptyRoots{})
	roots := Slots{(*Root)(&a)}
	before := slices.Clone(c.edenRegions)
	entry := *c.entry(a)
	cleanup := armFailure(c, failPromotionPlan, 0)
	err := c.CollectMinor(roots)
	cleanup()
	if err == nil {
		t.Fatal("missing injected failure")
	}
	if !slices.Equal(before, c.edenRegions) || entry != *c.entry(a) {
		t.Fatal("failed evacuation reset live region")
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
}
