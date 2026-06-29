package gc

import "testing"

func TestProfileNormalization(t *testing.T) {
	c := newTestCollector(t, Config{})
	if c.cfg.Profile != ProfileThroughput || c.cfg.Allocator != AllocatorPagedSizeClass || c.cfg.Runtime != RuntimeGenerational {
		t.Fatalf("zero config normalized to %+v", c.cfg)
	}
	tiny := newTinyTestCollector(t, Config{Profile: ProfileTiny})
	if tiny.cfg.Allocator != AllocatorTinyFixedBlock || tiny.cfg.Runtime != RuntimeIncrementalMarkSweep {
		t.Fatalf("tiny config normalized to %+v", tiny.cfg)
	}
	if _, err := NewCollector(Config{Profile: ProfileTiny, Allocator: AllocatorPagedSizeClass, Runtime: RuntimeIncrementalMarkSweep}, testTypes(t)); err == nil {
		t.Fatal("expected invalid tiny allocator/runtime combination rejection")
	}
	if _, err := NewCollector(Config{Profile: ProfileThroughput, Allocator: AllocatorTinyFixedBlock, Runtime: RuntimeGenerational}, testTypes(t)); err == nil {
		t.Fatal("expected invalid throughput allocator/runtime combination rejection")
	}
}

func TestThroughputOldSpaceReuseAfterFullGC(t *testing.T) {
	c := newTestCollector(t, Config{TinyNurseryBytes: 96, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	a, _ := c.NewStructDefault(0)
	root := Root(a)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	off := c.entry(a).off
	root = Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	b, _ := c.NewStructDefault(0)
	root = Root(b)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(b).off != off {
		t.Fatalf("old space not reused: got off %d want %d", c.entry(b).off, off)
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}

func TestThroughputLargeObjectReuse(t *testing.T) {
	c := newTestCollector(t, Config{LargeObjectBytes: 64, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	a, err := c.NewArray(2, 32, I32Value(1))
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(a).space != spaceLarge {
		t.Fatal("array was not large")
	}
	off := c.entry(a).off
	root := Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	b, err := c.NewArray(2, 32, I32Value(2))
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(b).off != off {
		t.Fatalf("large space not reused: got off %d want %d", c.entry(b).off, off)
	}
}

func TestThroughputAllocatorFragmentationReuse(t *testing.T) {
	c := newTestCollector(t, Config{LargeObjectBytes: 64, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	a, _ := c.NewArray(2, 16, I32Value(1))
	b, _ := c.NewArray(2, 16, I32Value(1))
	offA := c.entry(a).off
	c.free(handleOf(a))
	c.free(handleOf(b))
	x, err := c.NewArray(2, 32, I32Value(3))
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(x).off != offA {
		// Coalesced large free spans should be reused before growing.
		t.Fatalf("large coalesced span not reused, off=%d", c.entry(x).off)
	}
}

func TestThroughputVerifyCatchesInvalidMetadata(t *testing.T) {
	c := newTestCollector(t, Config{})
	r, _ := c.NewStructDefault(0)
	root := Root(r)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	c.handles[handleOf(r)].off = uint32(len(c.throughput.mem)) + 8
	if err := c.Verify(Slots{&root}); err == nil {
		t.Fatal("expected verify to reject out-of-bounds throughput handle")
	}
}
