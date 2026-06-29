package gc

import "testing"

func TestThroughputOldAndLargeFreedMemoryIsPoisoned(t *testing.T) {
	c := newTestCollector(t, Config{PoisonFreed: true, StressNurseryBytes: 96, LargeObjectBytes: 128, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	obj, _ := c.NewStructDefault(0)
	root := Root(obj)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	e := *c.entry(root.GetRef())
	root = Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	for i, b := range c.throughput.mem[e.off : e.off+e.allocSize] {
		if b != 0xdd {
			t.Fatalf("old byte %d not poisoned: %#x", i, b)
		}
	}

	large, err := c.NewArray(2, 64, I32Value(7))
	if err != nil {
		t.Fatal(err)
	}
	root = Root(large)
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	e = *c.entry(root.GetRef())
	root = Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	for i, b := range c.throughput.mem[e.off : e.off+e.allocSize] {
		if b != 0xdd {
			t.Fatalf("large byte %d not poisoned: %#x", i, b)
		}
	}
}
