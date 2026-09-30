//go:build wago_tiny_nonincremental

package gc

import "testing"

func TestTinyNonIncrementalPacingDoesNotExposeReservation(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf})
	roots := make([]Root, 0, 65)
	for i := 0; i < 65; i++ {
		object, err := c.NewStructDefaultWithRoots(0, tinyRootSliceSlots(roots))
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, Root(object))
	}
}

func TestTinyNonIncrementalStepCompletesCycle(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf})
	keep, _ := c.NewStructDefault(0)
	drop, _ := c.NewStructDefault(0)
	root := Root(keep)
	if err := c.Step(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.state != tinyIdle || !c.validObjectRef(keep) || c.validObjectRef(drop) {
		t.Fatalf("nonincremental Step state/live = %d/%v/%v", c.tinyGC.state, c.validObjectRef(keep), c.validObjectRef(drop))
	}
	if c.stats.FullCollections != 1 {
		t.Fatalf("nonincremental Step full collections = %d, want 1", c.stats.FullCollections)
	}
}
