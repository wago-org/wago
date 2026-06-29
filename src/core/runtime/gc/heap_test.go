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
	c, err := NewCollector(cfg, testTypes(t))
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
