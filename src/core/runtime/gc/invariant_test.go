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
