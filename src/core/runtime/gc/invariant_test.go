package gc

import "testing"

func nonNullTypes(t *testing.T) []TypeDesc {
	t.Helper()
	pf, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	nn, err := NewStructDesc(1, []StorageKind{StorageRef})
	if err != nil {
		t.Fatal(err)
	}
	nna, err := NewArrayDesc(2, StorageRef)
	if err != nil {
		t.Fatal(err)
	}
	return []TypeDesc{pf, nn, nna}
}

func TestRememberedSetPrunedWhenOldObjectDies(t *testing.T) {
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
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if c.RememberedCount() != 0 {
		t.Fatalf("stale remembered entries after full GC: %d", c.RememberedCount())
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
}

func TestMinorGCDrainsRememberedTransitiveYoungGraph(t *testing.T) {
	c := newTestCollector(t, Config{VerifyAfterCollect: true})
	old, _ := c.NewStructDefault(1)
	if err := c.ForcePromote(old); err != nil {
		t.Fatal(err)
	}
	parent, _ := c.NewStructDefault(1)
	child, _ := c.NewStructDefault(0)
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(old, 0, RefValue(parent)); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if c.entry(parent).space == spaceFree || c.entry(child).space == spaceFree {
		t.Fatalf("remembered transitive young graph not preserved: parent=%v child=%v", c.entry(parent).space, c.entry(child).space)
	}
	if c.entry(parent).space != spaceOld || c.entry(child).space != spaceOld {
		t.Fatalf("young survivors not promoted: parent=%v child=%v", c.entry(parent).space, c.entry(child).space)
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsInvalidCardMetadata(t *testing.T) {
	c := newTestCollector(t, Config{})
	arr, err := c.NewArrayDefault(3, 2)
	if err != nil {
		t.Fatal(err)
	}
	young, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	g := c.NewGlobalSlot(Null())
	tab := c.NewTableSlot(Null())
	c.CardMarkArray(arr, 1)
	if err := c.SetGlobalSlot(g, young); err != nil {
		t.Fatal(err)
	}
	if err := c.SetTableSlot(tab, young); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(nil); err != nil {
		t.Fatalf("valid card metadata failed verify: %v", err)
	}

	validObjectCards := append([]objectCard(nil), c.objectCards...)
	validSlotCards := append([]slotCard(nil), c.slotCards...)

	c.objectCards = append(validObjectCards[:0:0], objectCard{handle: 0, index: 0})
	if err := c.Verify(nil); err == nil {
		t.Fatal("Verify accepted zero object-card handle")
	}
	c.objectCards = append(validObjectCards[:0:0], objectCard{handle: uint32(len(c.handles)), index: 0})
	if err := c.Verify(nil); err == nil {
		t.Fatal("Verify accepted out-of-range object-card handle")
	}
	c.objectCards = append(validObjectCards[:0:0], objectCard{handle: handleOf(arr), index: 0})
	c.slotCards = validSlotCards
	root := Root(Null())
	if err := c.SetGlobalSlot(g, Null()); err != nil {
		t.Fatal(err)
	}
	if err := c.SetTableSlot(tab, Null()); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	c.objectCards = []objectCard{{handle: handleOf(arr), index: 0}}
	if err := c.Verify(nil); err == nil {
		t.Fatal("Verify accepted stale object-card handle")
	}

	c = newTestCollector(t, Config{})
	young, err = c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	c.WriteBarrierSlot(SlotGlobal, ^uint32(0), young)
	if len(c.slotCards) != 0 {
		t.Fatalf("out-of-range slot barrier recorded %d cards", len(c.slotCards))
	}
	c.slotCards = []slotCard{{kind: SlotFrame, index: 0}}
	if err := c.Verify(nil); err == nil {
		t.Fatal("Verify accepted unsupported frame slot card")
	}
	c.slotCards = []slotCard{{kind: SlotGlobal, index: 0}}
	if err := c.Verify(nil); err == nil {
		t.Fatal("Verify accepted out-of-range global slot card")
	}
	_ = c.NewGlobalSlot(Null())
	c.slotCards = []slotCard{{kind: SlotTable, index: 0}}
	if err := c.Verify(nil); err == nil {
		t.Fatal("Verify accepted out-of-range table slot card")
	}
}

func TestRememberedHandleReuseDoesNotScanUnrelatedObject(t *testing.T) {
	c := newTestCollector(t, Config{})
	old, _ := c.NewStructDefault(1)
	if err := c.ForcePromote(old); err != nil {
		t.Fatal(err)
	}
	young, _ := c.NewStructDefault(0)
	if err := c.StructSet(old, 0, RefValue(young)); err != nil {
		t.Fatal(err)
	}
	oldHandle := handleOf(old)
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if c.RememberedCount() != 0 {
		t.Fatalf("remembered not pruned")
	}
	var reused Ref
	for i := 0; i < 3; i++ {
		r, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if handleOf(r) == oldHandle {
			reused = r
			break
		}
	}
	if reused.IsNull() {
		t.Fatalf("test expected handle %d to be reused", oldHandle)
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if c.entry(reused).space != spaceFree {
		t.Fatal("stale remembered handle kept unrelated object alive")
	}
}

func TestNonNullRefDefaultAndSetRejected(t *testing.T) {
	c, err := NewCollector(Config{}, nonNullTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.NewStructDefault(1); err == nil {
		t.Fatal("non-null ref struct default succeeded")
	}
	if _, err := c.NewArrayDefault(2, 1); err == nil {
		t.Fatal("non-null ref array default succeeded")
	}

	d, _ := c.desc(1)
	sz, _ := StructSize(d)
	obj, err := c.alloc(d, sz, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(obj, 0, RefValue(Null())); err == nil {
		t.Fatal("null stored into non-null struct field")
	}

	child, _ := c.NewStructDefault(0)
	arr, err := c.NewArray(2, 1, RefValue(child))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(arr, 0, RefValue(Null())); err == nil {
		t.Fatal("null stored into non-null array element")
	}
}

func TestInvalidStoresDoNotRunBarriers(t *testing.T) {
	c := newTestCollector(t, Config{})
	old, _ := c.NewStructDefault(1)
	if err := c.ForcePromote(old); err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(old, 0, I32Value(1)); err == nil {
		t.Fatal("invalid struct store succeeded")
	}
	if c.RememberedCount() != 0 {
		t.Fatalf("invalid struct store ran barrier: remembered=%d", c.RememberedCount())
	}
	arr, _ := c.NewArrayDefault(3, 1)
	if err := c.ForcePromote(arr); err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(arr, 0, I32Value(1)); err == nil {
		t.Fatal("invalid array store succeeded")
	}
	if c.CardCount() != 0 {
		t.Fatalf("invalid array store marked card: cards=%d", c.CardCount())
	}
}

func TestNewArrayPrechecksInitializerCompatibility(t *testing.T) {
	c := newTestCollector(t, Config{})
	before := c.Stats().LiveObjects
	if _, err := c.NewArray(3, 2, I32Value(1)); err == nil {
		t.Fatal("invalid ref array initializer succeeded")
	}
	if after := c.Stats().LiveObjects; after != before {
		t.Fatalf("invalid initializer allocated object: before=%d after=%d", before, after)
	}
}

func TestStoreRejectsIncompatibleValueKind(t *testing.T) {
	c := newTestCollector(t, Config{})
	obj, _ := c.NewStructDefault(0)
	if err := c.StructSet(obj, 0, I64Value(1)); err == nil {
		t.Fatal("stored i64 into i32 field")
	}
	arr, _ := c.NewArrayDefault(3, 1)
	if err := c.ArraySet(arr, 0, I32Value(1)); err == nil {
		t.Fatal("stored numeric into ref array")
	}
}

func TestLoadStoreBoundsChecksDoNotPanic(t *testing.T) {
	c := newTestCollector(t, Config{})
	obj, _ := c.NewStructDefault(0)
	off := uint64(c.entry(obj).size - 1)
	if _, err := c.loadValue(obj, off, StorageI64); err == nil {
		t.Fatal("expected load bounds error")
	}
	if err := c.storeValue(obj, TypeDesc{}, off, StorageI64, I64Value(1)); err == nil {
		t.Fatal("expected store bounds error")
	}
}

func TestAllocationTriggeredCollectionRequiresRoots(t *testing.T) {
	c := newTestCollector(t, Config{CollectEveryAlloc: true})
	if _, err := c.NewStructDefault(0); err == nil {
		t.Fatal("collect-every-alloc without roots succeeded")
	}
	if _, err := c.NewStructDefaultWithRoots(0, Slots{}); err != nil {
		t.Fatalf("collect-every-alloc with explicit roots failed: %v", err)
	}
}
