package gc

import (
	"errors"
	"testing"
)

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

func TestCheckedRootSlotConstructorsValidateInitialRefs(t *testing.T) {
	cases := []struct {
		name string
		new  func(*testing.T) *Collector
	}{
		{name: "throughput", new: func(t *testing.T) *Collector {
			return newTestCollector(t, Config{StressNurseryBytes: 128, VerifyAfterCollect: true})
		}},
		{name: "tiny", new: func(t *testing.T) *Collector {
			return newTinyTestCollector(t, Config{TinyHeapBytes: 1024, VerifyAfterCollect: true})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.new(t)
			live, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			g, err := c.NewCheckedGlobalSlot(live)
			if err != nil {
				t.Fatalf("valid global initial ref rejected: %v", err)
			}
			tab, err := c.NewCheckedTableSlot(live)
			if err != nil {
				t.Fatalf("valid table initial ref rejected: %v", err)
			}
			if _, err := c.NewCheckedGlobalSlot(Null()); err != nil {
				t.Fatalf("null global initial ref rejected: %v", err)
			}
			if _, err := c.NewCheckedTableSlot(I31New(-7)); err != nil {
				t.Fatalf("i31 table initial ref rejected: %v", err)
			}

			dead, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.CollectFull(nil); err != nil {
				t.Fatal(err)
			}
			if c.entry(live).space == spaceFree {
				t.Fatal("checked root slot initial refs did not root live object")
			}
			if c.entry(dead).space != spaceFree {
				t.Fatal("test setup failed to free unrooted object")
			}
			beforeGlobals, beforeTables := len(c.globalSlots), len(c.tableSlots)
			if _, err := c.NewCheckedGlobalSlot(dead); err == nil {
				t.Fatal("checked global constructor accepted freed ref")
			}
			if _, err := c.NewCheckedTableSlot(dead); err == nil {
				t.Fatal("checked table constructor accepted freed ref")
			}
			forged := Ref(0xffff << 1)
			if _, err := c.NewCheckedGlobalSlot(forged); err == nil {
				t.Fatal("checked global constructor accepted forged ref")
			}
			if _, err := c.NewCheckedTableSlot(forged); err == nil {
				t.Fatal("checked table constructor accepted forged ref")
			}
			if len(c.globalSlots) != beforeGlobals || len(c.tableSlots) != beforeTables {
				t.Fatalf("rejected initial refs changed slot counts: globals %d->%d tables %d->%d", beforeGlobals, len(c.globalSlots), beforeTables, len(c.tableSlots))
			}
			if err := c.SetGlobalSlot(g, dead); err == nil {
				t.Fatal("global setter accepted freed ref")
			}
			if err := c.SetTableSlot(tab, dead); err == nil {
				t.Fatal("table setter accepted freed ref")
			}
		})
	}
}

func TestUncheckedRootSlotConstructorsPanicOnInvalidInitialRefs(t *testing.T) {
	mustPanic := func(t *testing.T, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatal("constructor did not panic")
			}
		}()
		fn()
	}

	c := newTestCollector(t, Config{})
	forged := Ref(0xffff << 1)
	mustPanic(t, func() { _ = c.NewGlobalSlot(forged) })
	mustPanic(t, func() { _ = c.NewTableSlot(forged) })
	if len(c.globalSlots) != 0 || len(c.tableSlots) != 0 {
		t.Fatalf("panicking constructors appended slots: globals=%d tables=%d", len(c.globalSlots), len(c.tableSlots))
	}
}

func TestClosedCollectorRejectsLiveOperations(t *testing.T) {
	cases := []struct {
		name string
		new  func(*testing.T) *Collector
	}{
		{name: "throughput", new: func(t *testing.T) *Collector {
			return newTestCollector(t, Config{StressNurseryBytes: 128, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
		}},
		{name: "tiny", new: func(t *testing.T) *Collector {
			return newTinyTestCollector(t, Config{TinyHeapBytes: 1024})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.new(t)
			obj, err := c.NewStructDefault(1)
			if err != nil {
				t.Fatal(err)
			}
			arr, err := c.NewArrayDefault(3, 1)
			if err != nil {
				t.Fatal(err)
			}
			g := c.NewGlobalSlot(obj)
			tab := c.NewTableSlot(obj)
			root := Root(obj)
			if err := c.CollectFull(Slots{&root}); err != nil {
				t.Fatal(err)
			}
			obj = Ref(root)
			before := c.Stats()
			c.Close()
			c.Close()

			mustClosed := func(label string, err error) {
				t.Helper()
				if !errors.Is(err, errCollectorClosed) {
					t.Fatalf("%s error = %v, want collector closed", label, err)
				}
			}
			mustClosed("new struct", func() error { _, err := c.NewStructDefault(0); return err }())
			mustClosed("new array", func() error { _, err := c.NewArrayDefault(3, 1); return err }())
			mustClosed("collect full", c.CollectFull(nil))
			mustClosed("collect minor", c.CollectMinor(nil))
			mustClosed("step", c.Step(nil))
			mustClosed("verify", c.Verify(nil))
			mustClosed("force promote", c.ForcePromote(obj))
			mustClosed("struct get", func() error { _, err := c.StructGet(obj, 0); return err }())
			mustClosed("struct set", c.StructSet(obj, 0, RefValue(Null())))
			mustClosed("array len", func() error { _, err := c.ArrayLen(arr); return err }())
			mustClosed("array get", func() error { _, err := c.ArrayGet(arr, 0); return err }())
			mustClosed("array set", c.ArraySet(arr, 0, RefValue(Null())))
			mustClosed("new global", func() error { _, err := c.NewCheckedGlobalSlot(Null()); return err }())
			mustClosed("new table", func() error { _, err := c.NewCheckedTableSlot(Null()); return err }())
			mustClosed("set global", c.SetGlobalSlot(g, Null()))
			mustClosed("set table", c.SetTableSlot(tab, Null()))
			mustClosed("checked global", func() error { _, err := c.CheckedGlobalSlot(g); return err }())
			mustClosed("checked table", func() error { _, err := c.CheckedTableSlot(tab); return err }())

			if got := c.GlobalSlot(g); !got.IsNull() {
				t.Fatalf("GlobalSlot after Close = %#x, want null", got)
			}
			if got := c.TableSlot(tab); !got.IsNull() {
				t.Fatalf("TableSlot after Close = %#x, want null", got)
			}
			after := c.Stats()
			if after.MinorCollections != before.MinorCollections || after.FullCollections != before.FullCollections || after.Allocations != before.Allocations {
				t.Fatalf("closed operations mutated stats: before=%+v after=%+v", before, after)
			}
		})
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
	slotOff := c.throughput.freeSlots[cls][idx].off
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
