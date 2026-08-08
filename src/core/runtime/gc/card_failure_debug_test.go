//go:build wagodebug

package gc

import "testing"

func TestInjectedObjectCardGrowthFallsBackToWholeObjectScan(t *testing.T) {
	c := newTestCollector(t, Config{VerifyAfterCollect: true})
	array, err := c.NewArrayDefault(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := armFailure(c, failObjectCardGrowth, 0)
	if err := c.ArraySet(array, 0, RefValue(child)); err != nil {
		cleanup()
		t.Fatal(err)
	}
	cleanup()
	if c.RememberedCount() != 1 || c.handles[handleOf(array)].cardSlot != 0 {
		t.Fatalf("object-card fallback metadata remembered=%d slot=%d", c.RememberedCount(), c.handles[handleOf(array)].cardSlot)
	}
	root := Root(array)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(child).space != spaceOld {
		t.Fatalf("fallback-scanned child space=%v, want old", c.entry(child).space)
	}
}

func TestInjectedRootCardGrowthFallsBackToAllPersistentSlots(t *testing.T) {
	c := newTestCollector(t, Config{VerifyAfterCollect: true})
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	slot := c.NewGlobalSlot(Null())
	cleanup := armFailure(c, failSlotCardGrowth, 0)
	if err := c.SetGlobalSlot(slot, child); err != nil {
		cleanup()
		t.Fatal(err)
	}
	cleanup()
	if !c.rootCardFallback || len(c.slotCards) != 0 {
		t.Fatalf("root-card fallback=%v cards=%v", c.rootCardFallback, c.slotCards)
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if c.entry(child).space != spaceOld {
		t.Fatalf("fallback-rooted child space=%v, want old", c.entry(child).space)
	}
	if c.rootCardFallback {
		t.Fatal("minor collection did not clear root-card fallback")
	}
}
