//go:build !wago_tiny_nonincremental

package gc

import "testing"

func TestTinyAllocationUsesSweptSpaceWithoutDrainingCycle(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 16 * 512, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf})
	objects := make([]Ref, 300)
	for i := range objects {
		objects[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	root := Root(objects[len(objects)-1])
	for c.tinyGC.state != tinySweep {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Step(Slots{&root}); err != nil { // reclaim at least one early span.
		t.Fatal(err)
	}
	if c.tinyGC.state != tinySweep {
		t.Fatal("test sweep completed before allocation")
	}
	before := c.tinyGC.sweep
	allocated, err := c.NewStructDefaultWithRoots(0, Slots{&root})
	if err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.state != tinySweep {
		t.Fatal("allocation synchronously drained Tiny sweep")
	}
	if c.tinyGC.sweep < before || c.tinyGC.sweep-before > tinyStepSweepHandles {
		t.Fatalf("allocation sweep progress %d -> %d exceeded one bounded debt Step", before, c.tinyGC.sweep)
	}
	if !c.validObjectRef(allocated) {
		t.Fatal("allocation from swept space is invalid")
	}
}

func TestTinySweepStepUsesBoundedHandleAndBlockWork(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 16 * 2048, TinyBlockBytes: 16}, []TypeDesc{leaf})
	for i := 0; i < 1000; i++ {
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	for c.tinyGC.state != tinySweep {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	before := c.tinyGC.sweep
	if err := c.Step(nil); err != nil {
		t.Fatal(err)
	}
	advanced := c.tinyGC.sweep - before
	if advanced == 0 || advanced > 64 {
		t.Fatalf("sweep advanced %d handles, want 1..64", advanced)
	}
	if c.tinyGC.state != tinySweep {
		t.Fatal("one bounded sweep Step completed a 1,000-handle sweep")
	}
}

func TestTinyPoisonSweepBoundsConfiguredBlockBytes(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 16 << 10, TinyBlockBytes: 8 << 10, PoisonFreed: true}, []TypeDesc{leaf})
	object, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	for c.tinyGC.state != tinySweep {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Step(nil); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.scan.handle != handleOf(object) || c.tinyGC.scan.scan.index != tinyStepSweepBytes {
		t.Fatalf("large-block poison cursor = %+v, want handle %d byte %d", c.tinyGC.scan, handleOf(object), tinyStepSweepBytes)
	}
}

func TestTinyPoisonSweepResumesLargeSpan(t *testing.T) {
	array, err := NewArrayDesc(0, StorageI64)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 16 * 2048, TinyBlockBytes: 16, PoisonFreed: true}, []TypeDesc{array})
	object, err := c.NewArrayDefault(0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	for c.tinyGC.state != tinySweep {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Step(nil); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(object) {
		t.Fatal("large poisoned object was freed in one sweep Step")
	}
	if c.tinyGC.state != tinySweep {
		t.Fatal("large poisoned sweep did not remain resumable")
	}
	cursor := c.tinyGC.scan
	if err := c.CollectFull(nil); err == nil {
		t.Fatal("CollectFull restarted a partially poisoned sweep object")
	}
	if c.tinyGC.scan != cursor || !c.validObjectRef(object) {
		t.Fatal("rejected poison restart changed the active reclamation cursor")
	}
	global := c.NewGlobalSlot(Null())
	if err := c.SetGlobalSlot(global, object); err == nil {
		t.Fatal("checked root published an object during bounded poison reclamation")
	}
	if !c.GlobalSlot(global).IsNull() {
		t.Fatal("rejected bounded-poison publication mutated the root")
	}
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	if c.validObjectRef(object) {
		t.Fatal("large poisoned object survived completed sweep")
	}
}
