package gc

import "testing"

func testTypes(t *testing.T) []TypeDesc {
	t.Helper()
	pf, err := NewStructDesc(0, []StorageKind{StorageI32, StorageI64})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := NewStructDesc(1, []StorageKind{StorageRefNull, StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	ia, err := NewArrayDesc(2, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	ra, err := NewArrayDesc(3, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	return []TypeDesc{pf, pair, ia, ra}
}
func newTestCollector(t *testing.T, cfg Config) *Collector {
	t.Helper()
	return newTestCollectorWithTypes(t, cfg, testTypes(t))
}
func newTestCollectorWithTypes(t *testing.T, cfg Config, types []TypeDesc) *Collector {
	t.Helper()
	c, err := NewCollector(cfg, types)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestAllocationStructArrayAccess(t *testing.T) {
	c := newTestCollector(t, Config{})
	s, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(s, 0, I32Value(42)); err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(s, 1, I64Value(99)); err != nil {
		t.Fatal(err)
	}
	v, _ := c.StructGet(s, 0)
	if v.I32() != 42 {
		t.Fatalf("got %d", v.I32())
	}
	w, _ := c.StructGet(s, 1)
	if w.I64() != 99 {
		t.Fatalf("got %d", w.I64())
	}
	a, err := c.NewArray(2, 4, I32Value(7))
	if err != nil {
		t.Fatal(err)
	}
	ln, _ := c.ArrayLen(a)
	if ln != 4 {
		t.Fatalf("len %d", ln)
	}
	for i := uint32(0); i < 4; i++ {
		v, _ := c.ArrayGet(a, i)
		if v.I32() != 7 {
			t.Fatalf("idx %d", i)
		}
	}
	if err := c.ArraySet(a, 2, I32Value(11)); err != nil {
		t.Fatal(err)
	}
	v, _ = c.ArrayGet(a, 2)
	if v.I32() != 11 {
		t.Fatal("set failed")
	}
}

func TestArrayInitializerRefSurvivesAllocationCollection(t *testing.T) {
	c := newTestCollector(t, Config{})
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(child, 0, I32Value(42)); err != nil {
		t.Fatal(err)
	}
	c.cfg.CollectEveryAlloc = true
	array, err := c.NewArrayWithRoots(3, 1, RefValue(child), Slots{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.ArrayGet(array, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref == array {
		t.Fatal("array initializer was collected and handle was reused for the new array")
	}
	field, err := c.StructGet(got.Ref, 0)
	if err != nil {
		t.Fatalf("array element does not reference preserved struct: %v", err)
	}
	if field.I32() != 42 {
		t.Fatalf("field = %d, want 42", field.I32())
	}
}

func TestFullCollectionRootsChainsAndCycles(t *testing.T) {
	c := newTestCollector(t, Config{PoisonFreed: true})
	a, _ := c.NewStructDefault(1)
	b, _ := c.NewStructDefault(1)
	dead, _ := c.NewStructDefault(1)
	_ = c.StructSet(a, 0, RefValue(b))
	_ = c.StructSet(b, 0, RefValue(a))
	_ = c.StructSet(dead, 0, RefValue(dead))
	root := Root(a)
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 2 {
		t.Fatalf("live=%d", c.Stats().LiveObjects)
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	_ = dead
}

func TestUnrootedReclaimedAndVerifyFailure(t *testing.T) {
	c := newTestCollector(t, Config{})
	obj, _ := c.NewStructDefault(0)
	root := Root(obj)
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 0 {
		t.Fatalf("live=%d", c.Stats().LiveObjects)
	}
	if err := c.Verify(Slots{&root}); err == nil {
		t.Fatal("expected invalid root failure")
	}
}

func TestMinorPromotesRootAndSurvives(t *testing.T) {
	c := newTestCollector(t, Config{StressNurseryBytes: 128})
	a, _ := c.NewStructDefault(1)
	b, _ := c.NewStructDefault(0)
	_ = c.StructSet(a, 0, RefValue(b))
	root := Root(a)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(a).space != spaceOld || c.entry(b).space != spaceOld {
		t.Fatal("survivors not promoted")
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}

func TestExactScanning(t *testing.T) {
	c := newTestCollector(t, Config{})
	child, _ := c.NewStructDefault(0)
	pf, _ := c.NewStructDefault(0)
	// Store bits that look like a valid object ref in a pointer-free object; exact
	// scanning must not keep child alive through numeric payload.
	_ = c.StructSet(pf, 0, I32Value(int32(child)))
	root := Root(pf)
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(child).space != spaceFree {
		t.Fatal("numeric lookalike kept child alive")
	}

	c = newTestCollector(t, Config{})
	child, _ = c.NewStructDefault(0)
	parent, _ := c.NewStructDefault(1)
	_ = c.StructSet(parent, 0, RefValue(child))
	_ = c.StructSet(parent, 1, RefValue(I31New(-3)))
	root = Root(parent)
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(child).space == spaceFree {
		t.Fatal("ref field did not keep child")
	}

	arr, _ := c.NewArrayDefault(3, 2)
	child2, _ := c.NewStructDefault(0)
	_ = c.ArraySet(arr, 0, RefValue(child2))
	r2 := Root(arr)
	if err := c.CollectFull(Slots{&root, &r2}); err != nil {
		t.Fatal(err)
	}
	if c.entry(child2).space == spaceFree {
		t.Fatal("ref array did not keep child")
	}
}

func TestMinorKeepsNurseryChildStoredInLargeParent(t *testing.T) {
	childDesc, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	largeFields := make([]StorageKind, 20)
	for i := range largeFields {
		largeFields[i] = StorageRefNull
	}
	largeStruct, err := NewStructDesc(1, largeFields)
	if err != nil {
		t.Fatal(err)
	}
	largeArray, err := NewArrayDesc(2, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		store func(*Collector, Ref) (parent Ref, child Ref, err error)
		load  func(*Collector, Ref) (Ref, error)
	}{
		{
			name: "struct field",
			store: func(c *Collector, child Ref) (Ref, Ref, error) {
				parent, err := c.NewStructDefault(1)
				if err != nil {
					return Null(), Null(), err
				}
				return parent, child, c.StructSet(parent, 0, RefValue(child))
			},
			load: func(c *Collector, parent Ref) (Ref, error) {
				v, err := c.StructGet(parent, 0)
				return v.Ref, err
			},
		},
		{
			name: "array element",
			store: func(c *Collector, child Ref) (Ref, Ref, error) {
				parent, err := c.NewArrayDefault(2, 16)
				if err != nil {
					return Null(), Null(), err
				}
				return parent, child, c.ArraySet(parent, 15, RefValue(child))
			},
			load: func(c *Collector, parent Ref) (Ref, error) {
				v, err := c.ArrayGet(parent, 15)
				return v.Ref, err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCollectorWithTypes(t, Config{LargeObjectBytes: 64, VerifyAfterCollect: true}, []TypeDesc{childDesc, largeStruct, largeArray})
			child, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			parent, child, err := tc.store(c, child)
			if err != nil {
				t.Fatal(err)
			}
			if c.entry(parent).space != spaceLarge {
				t.Fatalf("parent space=%v, want large", c.entry(parent).space)
			}
			if c.entry(child).space != spaceNursery {
				t.Fatalf("child space=%v, want nursery", c.entry(child).space)
			}
			if c.RememberedCount() != 1 {
				t.Fatalf("remembered=%d, want 1", c.RememberedCount())
			}

			if err := c.CollectMinor(nil); err != nil {
				t.Fatal(err)
			}
			if c.entry(child).space != spaceOld {
				t.Fatalf("large parent did not preserve nursery child; child space=%v", c.entry(child).space)
			}
			got, err := tc.load(c, parent)
			if err != nil {
				t.Fatal(err)
			}
			if got != child {
				t.Fatalf("stored child ref=%v, want %v", got, child)
			}
		})
	}
}

func TestCardMetadataRetainsFullIndexes(t *testing.T) {
	c := newTestCollector(t, Config{})
	arr, err := c.NewArrayDefault(3, 1)
	if err != nil {
		t.Fatal(err)
	}

	const elementIndex = uint32(0x1_0001)
	c.CardMarkArray(arr, elementIndex)
	if len(c.objectCards) != 1 {
		t.Fatalf("object cards=%d, want 1", len(c.objectCards))
	}
	if got := c.objectCards[0].index; got != elementIndex {
		t.Fatalf("object card index=%#x, want %#x", got, elementIndex)
	}

	c.BulkWriteBarrier(arr, ^uint32(0)-1, 4)
	if len(c.objectCards) != 3 {
		t.Fatalf("object cards=%d, want 3", len(c.objectCards))
	}
	if got := c.objectCards[1].index; got != ^uint32(0)-1 {
		t.Fatalf("bulk start index=%#x, want %#x", got, ^uint32(0)-1)
	}
	if got := c.objectCards[2].index; got != ^uint32(0) {
		t.Fatalf("bulk end index=%#x, want saturated %#x", got, ^uint32(0))
	}
}

func TestSlotCardsAreNotRemovedAsObjectCards(t *testing.T) {
	c := newTestCollector(t, Config{})
	young, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	const slotIndex = uint32(0x1_0000)
	c.WriteBarrierSlot(SlotGlobal, slotIndex, young)
	if len(c.slotCards) != 1 {
		t.Fatalf("slot cards=%d, want 1", len(c.slotCards))
	}
	if got := c.slotCards[0].index; got != slotIndex {
		t.Fatalf("slot card index=%#x, want %#x", got, slotIndex)
	}

	// The old packed uint32 representation made this slot card look like object
	// handle SlotGlobal<<8|1 to removeCardsForHandle.
	c.removeCardsForHandle(uint32(SlotGlobal)<<8 | 1)
	if len(c.slotCards) != 1 {
		t.Fatalf("slot card removed as object card; remaining=%d", len(c.slotCards))
	}

	c.objectCards = append(c.objectCards, objectCard{handle: 7, index: 0})
	c.removeCardsForHandle(7)
	if len(c.objectCards) != 0 {
		t.Fatalf("object card for freed handle remained: %v", c.objectCards)
	}
	if len(c.slotCards) != 1 {
		t.Fatalf("object-card removal changed slot cards; remaining=%d", len(c.slotCards))
	}
}

func TestGlobalTableSlotAccessorsAreBoundsCheckedAndRoot(t *testing.T) {
	c := newTestCollector(t, Config{StressNurseryBytes: 128, VerifyAfterCollect: true})
	globalRef, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	tableRef, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}

	g := c.NewGlobalSlot(Null())
	tab := c.NewTableSlot(Null())
	if err := c.SetGlobalSlot(g, globalRef); err != nil {
		t.Fatal(err)
	}
	if err := c.SetTableSlot(tab, tableRef); err != nil {
		t.Fatal(err)
	}
	if got, err := c.CheckedGlobalSlot(g); err != nil || got != globalRef {
		t.Fatalf("checked global = %v, %v; want %v, nil", got, err, globalRef)
	}
	if got, err := c.CheckedTableSlot(tab); err != nil || got != tableRef {
		t.Fatalf("checked table = %v, %v; want %v, nil", got, err, tableRef)
	}

	for _, invalid := range []uint32{g + 1, ^uint32(0)} {
		if got := c.GlobalSlot(invalid); !got.IsNull() {
			t.Fatalf("invalid global slot %#x = %v, want null", invalid, got)
		}
		if _, err := c.CheckedGlobalSlot(invalid); err != errRange {
			t.Fatalf("checked invalid global slot %#x err=%v, want %v", invalid, err, errRange)
		}
		if err := c.SetGlobalSlot(invalid, Null()); err != errRange {
			t.Fatalf("set invalid global slot %#x err=%v, want %v", invalid, err, errRange)
		}
	}
	for _, invalid := range []uint32{tab + 1, ^uint32(0)} {
		if got := c.TableSlot(invalid); !got.IsNull() {
			t.Fatalf("invalid table slot %#x = %v, want null", invalid, got)
		}
		if _, err := c.CheckedTableSlot(invalid); err != errRange {
			t.Fatalf("checked invalid table slot %#x err=%v, want %v", invalid, err, errRange)
		}
		if err := c.SetTableSlot(invalid, Null()); err != errRange {
			t.Fatalf("set invalid table slot %#x err=%v, want %v", invalid, err, errRange)
		}
	}

	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if c.entry(globalRef).space != spaceOld || c.entry(tableRef).space != spaceOld {
		t.Fatalf("slot roots not promoted: global=%v table=%v", c.entry(globalRef).space, c.entry(tableRef).space)
	}
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if c.entry(globalRef).space == spaceFree || c.entry(tableRef).space == spaceFree {
		t.Fatalf("slot roots reclaimed: global=%v table=%v", c.entry(globalRef).space, c.entry(tableRef).space)
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
}

func TestSlotFrameBarrierUnsupportedDoesNotRoot(t *testing.T) {
	t.Run("throughput", func(t *testing.T) {
		c := newTestCollector(t, Config{})
		young, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		c.WriteBarrierSlot(SlotFrame, 0, young)
		if c.CardCount() != 0 {
			t.Fatalf("SlotFrame barrier recorded cards=%d", c.CardCount())
		}
		if err := c.CollectMinor(nil); err != nil {
			t.Fatal(err)
		}
		if c.entry(young).space != spaceFree {
			t.Fatalf("SlotFrame barrier rooted object in %v", c.entry(young).space)
		}
	})

	t.Run("tiny", func(t *testing.T) {
		c := newTinyTestCollector(t, Config{})
		child, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Step(nil); err != nil { // idle -> mark with no roots.
			t.Fatal(err)
		}
		c.WriteBarrierSlot(SlotFrame, 0, child)
		for c.tinyGC.state != tinyIdle {
			if err := c.Step(nil); err != nil {
				t.Fatal(err)
			}
		}
		if c.entry(child).space != spaceFree {
			t.Fatalf("SlotFrame barrier rooted tiny object in %v", c.entry(child).space)
		}
	})
}

func TestBarriersRememberOldToYoungAndSlots(t *testing.T) {
	c := newTestCollector(t, Config{})
	old, _ := c.NewStructDefault(1)
	if err := c.ForcePromote(old); err != nil {
		t.Fatal(err)
	}
	young, _ := c.NewStructDefault(0)
	if err := c.StructSet(old, 0, RefValue(young)); err != nil {
		t.Fatal(err)
	}
	if c.RememberedCount() != 1 {
		t.Fatalf("remembered=%d", c.RememberedCount())
	}
	arr, _ := c.NewArrayDefault(3, 2)
	_ = c.ForcePromote(arr)
	y2, _ := c.NewStructDefault(0)
	if err := c.ArraySet(arr, 1, RefValue(y2)); err != nil {
		t.Fatal(err)
	}
	if c.RememberedCount() != 2 || c.CardCount() == 0 {
		t.Fatalf("remembered=%d cards=%d", c.RememberedCount(), c.CardCount())
	}
	g := c.NewGlobalSlot(Null())
	before := c.CardCount()
	if err := c.SetGlobalSlot(g, young); err != nil {
		t.Fatal(err)
	}
	tab := c.NewTableSlot(Null())
	if err := c.SetTableSlot(tab, young); err != nil {
		t.Fatal(err)
	}
	if c.CardCount() < before+2 {
		t.Fatal("slot barriers did not mark cards")
	}
}

func TestStressCollectEveryAllocTinyNursery(t *testing.T) {
	c := newTestCollector(t, Config{StressNurseryBytes: 96, CollectEveryAlloc: true, VerifyAfterCollect: true})
	var roots []Root
	for i := 0; i < 20; i++ {
		slots := make([]RootSlot, len(roots))
		for j := range roots {
			slots[j] = &roots[j]
		}
		r, err := c.NewStructDefaultWithRoots(1, Slots(slots))
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, Root(r))
		if i > 0 {
			_ = c.StructSet(Ref(roots[i-1]), 0, RefValue(r))
		}
	}
	slots := make([]RootSlot, len(roots))
	for i := range roots {
		slots[i] = &roots[i]
	}
	if err := c.CollectFull(Slots(slots)); err != nil {
		t.Fatal(err)
	}
}
