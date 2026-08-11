//go:build !wago_tiny_nonincremental

package gc

import "testing"

func TestTinySweepBarrierQueuesBoundedMarkWork(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf, parentType})
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	for c.tinyGC.state != tinySweep {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyColorOf(handleOf(child)) != tinyWhite {
		t.Fatal("test child was not white at sweep")
	}
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.state != tinyMark || c.tinyColorOf(handleOf(child)) != tinyGray {
		t.Fatalf("sweep barrier state/color = %d/%d, want mark/gray", c.tinyGC.state, c.tinyColorOf(handleOf(child)))
	}
	if c.tinyGC.sweep == 0 {
		t.Fatal("sweep cursor was lost while barrier work was queued")
	}
	for c.tinyGC.state != tinySweep {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyColorOf(handleOf(child)) != tinyBlack {
		t.Fatal("bounded sweep barrier work did not complete before resuming sweep")
	}
}

func TestTinySweepInitializedAllocationQueuesTrace(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	large, err := NewArrayDesc(2, StorageI64)
	if err != nil {
		t.Fatal(err)
	}
	types := []TypeDesc{leaf, parentType, large}

	t.Run("ordinary sweep", func(t *testing.T) {
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 16, TinyBlockBytes: 16, VerifyAfterCollect: true}, types)
		child, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		for c.tinyGC.state != tinySweep {
			if err := c.Step(nil); err != nil {
				t.Fatal(err)
			}
		}
		parent, err := c.NewStructWithRoots(1, []Value{RefValue(child)}, EmptyRoots{})
		if err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.state != tinyMark || c.tinyGC.rootPhase != tinyRootsSweepBarrier || c.tinyColorOf(handleOf(parent)) != tinyGray {
			t.Fatalf("initialized sweep allocation state/phase/color = %d/%d/%d, want mark/sweep-barrier/gray", c.tinyGC.state, c.tinyGC.rootPhase, c.tinyColorOf(handleOf(parent)))
		}
		for c.tinyGC.state != tinyIdle {
			if err := c.Step(nil); err != nil {
				t.Fatal(err)
			}
		}
		assertTinyInitializedSweepGraph(t, c, parent, child)
	})

	t.Run("active poison cursor", func(t *testing.T) {
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 16, TinyBlockBytes: 16, PoisonFreed: true, VerifyAfterCollect: true}, types)
		if _, err := c.NewArrayDefault(2, 1024); err != nil {
			t.Fatal(err)
		}
		child, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		global := c.NewGlobalSlot(Null())
		for c.tinyGC.state != tinySweep {
			if err := c.Step(nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.scan.handle == 0 {
			t.Fatal("test did not establish a bounded poison cursor")
		}
		parent, err := c.NewStructWithRoots(1, []Value{RefValue(child)}, EmptyRoots{})
		if err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.state != tinySweep || c.tinyGC.scan.handle == 0 || c.tinyColorOf(handleOf(parent)) != tinyGray {
			t.Fatalf("poisoned sweep allocation state/cursor/color = %d/%+v/%d, want sweep/active/gray", c.tinyGC.state, c.tinyGC.scan, c.tinyColorOf(handleOf(parent)))
		}
		poisonCursor := c.tinyGC.scan
		queued := len(c.tinyGC.grayStack)
		if err := c.SetGlobalSlot(global, parent); err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.scan != poisonCursor || len(c.tinyGC.grayStack) != queued || c.tinyColorOf(handleOf(parent)) != tinyGray {
			t.Fatalf("poison root barrier drained queued work: cursor=%+v stack=%d color=%d", c.tinyGC.scan, len(c.tinyGC.grayStack), c.tinyColorOf(handleOf(parent)))
		}
		if err := c.Verify(nil); err != nil {
			t.Fatalf("queued initialized allocation with poison cursor: %v", err)
		}
		for steps := 0; c.tinyGC.state == tinySweep; steps++ {
			if steps > 8 {
				t.Fatal("poison cursor did not yield to initialized allocation tracing")
			}
			if err := c.Step(nil); err != nil {
				t.Fatal(err)
			}
		}
		if c.tinyGC.state != tinyMark || c.tinyGC.rootPhase != tinyRootsSweepBarrier {
			t.Fatalf("post-poison state/phase = %d/%d, want mark/sweep-barrier", c.tinyGC.state, c.tinyGC.rootPhase)
		}
		for c.tinyGC.state != tinyIdle {
			if err := c.Step(nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.SetGlobalSlot(global, Null()); err != nil {
			t.Fatal(err)
		}
		assertTinyInitializedSweepGraph(t, c, parent, child)
	})
}

func assertTinyInitializedSweepGraph(t *testing.T, c *Collector, parent, child Ref) {
	t.Helper()
	if !c.validObjectRef(parent) || !c.validObjectRef(child) {
		t.Fatalf("initialized sweep graph live = parent:%v child:%v", c.validObjectRef(parent), c.validObjectRef(child))
	}
	value, err := c.StructGet(parent, 0)
	if err != nil || value.Ref != child {
		t.Fatalf("initialized sweep edge = %#x, %v; want %#x", value.Ref, err, child)
	}
	root := Root(parent)
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if c.validObjectRef(parent) || c.validObjectRef(child) {
		t.Fatal("next Tiny cycle did not reclaim the unrooted sweep allocation graph")
	}
}

func TestTinySweepRejectsWhitePointerfulObjectStore(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf, parentType})
	child, _ := c.NewStructDefault(0)
	whiteParent, _ := c.NewStructDefault(1)
	if err := c.StructSet(whiteParent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	blackParent, _ := c.NewStructDefault(1)
	root := Root(blackParent)
	for c.tinyGC.state != tinySweep {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := c.StructGet(blackParent, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(blackParent, 0, RefValue(whiteParent)); err == nil {
		t.Fatal("white pointerful graph was published into a sweep survivor")
	}
	after, err := c.StructGet(blackParent, 0)
	if err != nil || after != before {
		t.Fatalf("rejected sweep object store mutated field: before=%+v after=%+v err=%v", before, after, err)
	}
}
