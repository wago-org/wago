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
