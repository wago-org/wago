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
	c := newTestCollector(t, Config{StressNurseryBytes: 96, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
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

func TestThroughputOversizedNurseryObjectUsesLargeSpace(t *testing.T) {
	c := newTestCollector(t, Config{StressNurseryBytes: 64, LargeObjectBytes: 256, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	arr, err := c.NewArray(2, 16, I32Value(7)) // 16-byte header + 16*4-byte payload > 64-byte nursery.
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(arr).space != spaceLarge {
		t.Fatalf("oversized nursery object space=%v, want large", c.entry(arr).space)
	}
	if got, err := c.ArrayGet(arr, 15); err != nil || got.I32() != 7 {
		t.Fatalf("array element = %v, %v; want 7, nil", got, err)
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
}

func TestThroughputClassLimitMustBeSupportedSizeClass(t *testing.T) {
	c, err := NewCollector(Config{ThroughputClassLimit: 32}, testTypes(t))
	if err != nil {
		t.Fatalf("supported minimum class limit rejected: %v", err)
	}
	c.Close()
	for _, limit := range []uint32{16, 33, 65536} {
		if _, err := NewCollector(Config{ThroughputClassLimit: limit}, testTypes(t)); err == nil {
			t.Fatalf("unsupported class limit %d accepted", limit)
		}
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
