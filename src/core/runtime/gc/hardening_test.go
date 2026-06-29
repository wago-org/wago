package gc

import "testing"

func TestDefaultAllocationZerosReusedTinyPayload(t *testing.T) {
	c := newTinyTestCollector(t, Config{PoisonFreed: true, TinyHeapBytes: 1024})
	child, _ := c.NewStructDefault(0)
	obj, _ := c.NewStructDefault(1)
	if err := c.StructSet(obj, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	root := Root(child)
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	reused, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	v, err := c.StructGet(reused, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Ref.IsNull() {
		t.Fatalf("default ref field = %#x, want null", v.Ref)
	}

	arr, err := c.NewArray(2, 8, I32Value(0x12345678))
	if err != nil {
		t.Fatal(err)
	}
	root = Root(Null())
	_ = arr
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	arr, err = c.NewArrayDefault(2, 8)
	if err != nil {
		t.Fatal(err)
	}
	for i := uint32(0); i < 8; i++ {
		v, err := c.ArrayGet(arr, i)
		if err != nil {
			t.Fatal(err)
		}
		if v.I32() != 0 {
			t.Fatalf("default numeric element %d = %x", i, v.I32())
		}
	}
}

func TestDefaultAllocationZerosReusedThroughputPayload(t *testing.T) {
	c := newTestCollector(t, Config{PoisonFreed: true, StressNurseryBytes: 128, LargeObjectBytes: 128, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	child, _ := c.NewStructDefault(0)
	obj, _ := c.NewStructDefault(1)
	if err := c.StructSet(obj, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	root := Root(obj)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	root = Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	reused, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	root = Root(reused)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	v, err := c.StructGet(root.GetRef(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Ref.IsNull() {
		t.Fatalf("default old ref field = %#x, want null", v.Ref)
	}
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultAllocationZerosReusedThroughputLargePayload(t *testing.T) {
	c := newTestCollector(t, Config{PoisonFreed: true, LargeObjectBytes: 64, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	child, _ := c.NewStructDefault(0)
	arr, err := c.NewArray(3, 32, RefValue(child))
	if err != nil {
		t.Fatal(err)
	}
	root := Root(arr)
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	root = Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	arr, err = c.NewArrayDefault(3, 32)
	if err != nil {
		t.Fatal(err)
	}
	for i := uint32(0); i < 32; i++ {
		v, err := c.ArrayGet(arr, i)
		if err != nil {
			t.Fatal(err)
		}
		if !v.Ref.IsNull() {
			t.Fatalf("default large ref element %d = %#x", i, v.Ref)
		}
	}
}

func TestForgedRefsRejectedOrIgnoredNoPanic(t *testing.T) {
	c := newTestCollector(t, Config{})
	parent, _ := c.NewStructDefault(1)
	forged := Ref(0xffff << 1)
	if err := c.StructSet(parent, 0, RefValue(forged)); err == nil {
		t.Fatal("StructSet accepted forged ref")
	}
	arr, _ := c.NewArrayDefault(3, 1)
	if err := c.ArraySet(arr, 0, RefValue(forged)); err == nil {
		t.Fatal("ArraySet accepted forged ref")
	}
	c.WriteBarrierObject(parent, forged)
	c.WriteBarrierObject(forged, parent)
	g := c.NewGlobalSlot(Null())
	if err := c.SetGlobalSlot(g, forged); err == nil {
		t.Fatal("global accepted forged ref")
	}
	tab := c.NewTableSlot(Null())
	if err := c.SetTableSlot(tab, forged); err == nil {
		t.Fatal("table accepted forged ref")
	}
	c.globalSlots[g] = forged
	if err := c.Verify(nil); err == nil {
		t.Fatal("Verify accepted forged global metadata")
	}
}

func TestTinyBarrierDuringRemarkKeepsStoredChildAlive(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 1024, VerifyAfterCollect: true})
	parent, _ := c.NewStructDefault(1)
	child, _ := c.NewStructDefault(0)
	root := Root(parent)
	if err := c.Step(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	for c.tinyGC.state == tinyMark {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyGC.state != tinyRemark || c.tinyColorOf(handleOf(parent)) != tinyBlack {
		t.Fatalf("state=%v parent color=%v, want remark/black", c.tinyGC.state, c.tinyColorOf(handleOf(parent)))
	}
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StructGet(child, 0); err != nil {
		t.Fatalf("child was collected: %v", err)
	}
}

func TestTinySlotBarrierDuringRemarkKeepsStoredChildAlive(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 1024})
	parent, _ := c.NewStructDefault(1)
	child, _ := c.NewStructDefault(0)
	root := Root(parent)
	g := c.NewGlobalSlot(Null())
	tab := c.NewTableSlot(Null())
	if err := c.Step(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	for c.tinyGC.state == tinyMark {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.SetGlobalSlot(g, child); err != nil {
		t.Fatal(err)
	}
	if err := c.SetTableSlot(tab, child); err != nil {
		t.Fatal(err)
	}
	root = Root(Null())
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StructGet(child, 0); err != nil {
		t.Fatalf("slot child was collected: %v", err)
	}
}

func TestThroughputVerifyFreeSpanCorruption(t *testing.T) {
	c := newTestCollector(t, Config{StressNurseryBytes: 96, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, LargeObjectBytes: 128})
	a, _ := c.NewStructDefault(0)
	root := Root(a)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	root = Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	cls := c.throughput.classFor(Align8(StructSizeMust(testTypes(t)[0])))
	if cls < 0 || c.throughput.freeHeads[cls] == throughputNoSlot {
		t.Fatal("expected class free slot")
	}
	idx := c.throughput.freeHeads[cls]
	slotOff := c.throughput.freeSlots[cls][idx].off
	c.throughput.freeSlots[cls][idx].next = idx
	if err := c.Verify(nil); err == nil {
		t.Fatal("duplicate/cyclic class free slot passed verify")
	}

	c = newTestCollector(t, Config{StressNurseryBytes: 96, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, LargeObjectBytes: 128})
	a, _ = c.NewStructDefault(0)
	root = Root(a)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	root = Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	cls = c.throughput.classFor(Align8(StructSizeMust(testTypes(t)[0])))
	idx = c.throughput.freeHeads[cls]
	slotOff = c.throughput.freeSlots[cls][idx].off
	c.throughput.largeFree = append(c.throughput.largeFree, throughputLargeFree{off: slotOff, size: 64})
	if err := c.Verify(nil); err == nil {
		t.Fatal("class free slot overlapping large free span passed verify")
	}

	c = newTestCollector(t, Config{LargeObjectBytes: 64, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	large, _ := c.NewArray(2, 32, I32Value(1))
	root = Root(large)
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	root = Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if len(c.throughput.largeFree) == 0 {
		t.Fatal("expected large free span")
	}
	s := c.throughput.largeFree[0]
	c.throughput.largeFree = append(c.throughput.largeFree, throughputLargeFree{off: s.off + 8, size: 32})
	if err := c.Verify(nil); err == nil {
		t.Fatal("overlapping large free spans passed verify")
	}
}

func StructSizeMust(d TypeDesc) uint32 {
	sz, err := StructSize(d)
	if err != nil {
		panic(err)
	}
	return sz
}
