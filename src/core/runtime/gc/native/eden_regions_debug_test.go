//go:build wagodebug

package gc

import "testing"

func TestContiguousEdenPublicationRollback(t *testing.T) {
	c := regionCollector(t)
	regionStruct(t, c, 0, EmptyRoots{})
	bump := c.nurseryBump
	stats := c.Stats()
	cleanup := armFailure(c, failHandlePublication, 0)
	_, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
	cleanup()
	if err == nil {
		t.Fatal("missing injected failure")
	}
	if bump != c.nurseryBump || stats != c.Stats() {
		t.Fatal("publication failure consumed contiguous Eden")
	}
	regionStruct(t, c, 0, EmptyRoots{})
}

func TestContiguousEdenFailedEvacuationPreservesState(t *testing.T) {
	c := regionCollector(t)
	a := regionStruct(t, c, 0, EmptyRoots{})
	roots := Slots{(*Root)(&a)}
	entry := *c.entry(a)
	bump := c.nurseryBump
	cleanup := armFailure(c, failPromotionPlan, 0)
	err := c.CollectMinor(roots)
	cleanup()
	if err == nil {
		t.Fatal("missing injected failure")
	}
	if bump != c.nurseryBump || entry != *c.entry(a) {
		t.Fatal("failed evacuation changed contiguous Eden")
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
}

func TestContiguousEdenMinorFirstFailureUnwind(t *testing.T) {
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
	for c.Stats().MinorCollections == 0 {
		if err := allocate(); err != nil {
			t.Fatal(err)
		}
	}
	learned := c.Stats()
	if learned.FullCollections != 0 || learned.MinorCollections != 1 {
		t.Fatalf("setup did not collect minor-only: %+v", learned)
	}
	cleanup := armFailure(c, failPromotionPlan, 0)
	for i := 0; i < 32 && err == nil; i++ {
		err = allocate()
	}
	cleanup()
	if err == nil {
		t.Fatal("missing injected evacuation failure")
	}
	failed := c.Stats()
	if failed.FullCollections != learned.FullCollections {
		t.Fatalf("injected minor error was hidden by a full: before=%+v after=%+v", learned, failed)
	}
	if err := allocate(); err != nil {
		t.Fatal(err)
	}
	if c.Stats().FullCollections != learned.FullCollections {
		t.Fatalf("retry unexpectedly ran full: before=%+v after=%+v", learned, c.Stats())
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}
