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

func TestEdenRegionsExhaustionFeedbackFailureUnwind(t *testing.T) {
	c, err := NewCollector(Config{NurseryBytes: 128, NurseryRegionShift: 6, SurvivorBytes: 128}, testTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var root Root
	roots := Slots{&root}
	allocate := func() error {
		r, err := c.NewStructDefaultWithRoots(1, roots)
		if err != nil {
			return err
		}
		if err := c.StructSet(r, 0, RefValue(Ref(root))); err != nil {
			return err
		}
		root = Root(r)
		return nil
	}
	for i := 0; i < 5; i++ {
		if err := allocate(); err != nil {
			t.Fatal(err)
		}
	}
	learned := c.Stats()
	if learned.FullCollections != 1 || learned.MinorCollections != 1 {
		t.Fatalf("feedback setup did not full-then-minor: %+v", learned)
	}
	for i := 0; i < 3; i++ {
		if err := allocate(); err != nil {
			t.Fatal(err)
		}
	}

	cleanup := armFailure(c, failPromotionPlan, 0)
	err = allocate()
	cleanup()
	if err == nil {
		t.Fatal("missing injected evacuation failure")
	}
	failed := c.Stats()
	if failed.FullCollections != learned.FullCollections {
		t.Fatalf("failed learned path unexpectedly probed full: before=%+v after=%+v", learned, failed)
	}
	if err := allocate(); err != nil {
		t.Fatal(err)
	}
	if c.Stats().FullCollections != learned.FullCollections {
		t.Fatalf("failed collection consumed feedback decision: before=%+v after=%+v", learned, c.Stats())
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}
