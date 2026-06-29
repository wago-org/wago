package gc

import "testing"

func newTinyTestCollector(t *testing.T, cfg Config) *Collector {
	t.Helper()
	cfg.Policy = PolicyTiny
	if cfg.TinyHeapBytes == 0 {
		cfg.TinyHeapBytes = 256
	}
	if cfg.TinyBlockBytes == 0 {
		cfg.TinyBlockBytes = 16
	}
	c, err := NewCollector(cfg, testTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestTinyHeapInitializesOneFreeSpan(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 128, TinyBlockBytes: 16})
	if c.tiny.freeHead != 0 || c.tiny.blocks[0].size != 8 || c.tiny.blocks[0].used {
		t.Fatalf("bad initial span: head=%d span=%+v", c.tiny.freeHead, c.tiny.blocks[0])
	}
}

func TestTinyAllocateFreeReuseCoalesceAndFailure(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 96, TinyBlockBytes: 16, PoisonFreed: true})
	a, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := c.NewStructDefault(0)
	coff := c.entry(a).off
	c.free(handleOf(a))
	reuse, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(reuse).off != coff {
		t.Fatal("tiny allocator did not reuse freed span")
	}
	c.free(handleOf(reuse))
	c.free(handleOf(b))
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
	if c.tiny.freeHead != 0 || c.tiny.blocks[0].size != 6 {
		t.Fatalf("free spans not coalesced: head=%d span=%+v", c.tiny.freeHead, c.tiny.blocks[0])
	}
	for i := range c.tiny.mem[:32] {
		if c.tiny.mem[i] != 0xdd {
			t.Fatalf("freed byte %d not poisoned", i)
		}
	}
	roots := RefSliceRoots{}
	for i := 0; i < 3; i++ {
		r, err := c.NewStructDefaultWithRoots(0, roots)
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, r)
	}
	if _, err := c.NewStructDefaultWithRoots(0, roots); err == nil {
		t.Fatal("expected deterministic tiny heap exhaustion")
	}
}

func TestTinyFragmentationFailureThenCoalesceSucceeds(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 128, TinyBlockBytes: 16})
	var objs []Ref
	for i := 0; i < 4; i++ {
		r, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		objs = append(objs, r)
	}
	c.free(handleOf(objs[1]))
	c.free(handleOf(objs[3]))
	if _, err := c.NewArray(2, 8, I32Value(1)); err == nil { // 48 bytes, needs 3 contiguous blocks.
		t.Fatal("fragmented heap unexpectedly satisfied large allocation")
	}
	c.free(handleOf(objs[2]))
	if _, err := c.NewArray(2, 8, I32Value(1)); err != nil {
		t.Fatalf("coalesced heap did not satisfy allocation: %v", err)
	}
}

func TestTinyGCRootsCyclesExactScanningAndSlots(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 512, TinyBlockBytes: 16})
	child, _ := c.NewStructDefault(0)
	pf, _ := c.NewStructDefault(0)
	_ = c.StructSet(pf, 0, I32Value(int32(child)))
	root := Root(pf)
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(child).space != spaceFree {
		t.Fatal("numeric lookalike kept child alive under tiny")
	}

	a, _ := c.NewStructDefault(1)
	b, _ := c.NewStructDefault(1)
	_ = c.StructSet(a, 0, RefValue(b))
	_ = c.StructSet(b, 0, RefValue(a))
	_ = c.StructSet(a, 1, RefValue(I31New(7)))
	root = Root(a)
	arr, _ := c.NewArrayDefault(3, 2)
	leaf, _ := c.NewStructDefault(0)
	_ = c.ArraySet(arr, 0, RefValue(leaf))
	g := c.NewGlobalSlot(arr)
	tab := c.NewTableSlot(Null())
	if err := c.SetTableSlot(tab, b); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []Ref{a, b, arr, leaf, c.GlobalSlot(g), c.TableSlot(tab)} {
		if c.entry(r).space == spaceFree {
			t.Fatalf("live tiny ref %v reclaimed", r)
		}
	}
	root = Root(Null())
	_ = c.SetGlobalSlot(g, Null())
	_ = c.SetTableSlot(tab, Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 0 {
		t.Fatalf("unreachable tiny cycle survived: live=%d", c.Stats().LiveObjects)
	}
}

func TestTinyIncrementalStepAndBarriers(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 512, TinyBlockBytes: 16})
	parent, _ := c.NewStructDefault(1)
	child, _ := c.NewStructDefault(0)
	root := Root(parent)
	if err := c.Step(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.state != tinyMark || c.tinyColorOf(handleOf(parent)) != tinyGray {
		t.Fatal("step did not start tiny mark")
	}
	if err := c.Step(Slots{&root}); err != nil { // scan parent black while child is still white.
		t.Fatal(err)
	}
	if c.tinyColorOf(handleOf(parent)) != tinyBlack || c.tinyColorOf(handleOf(child)) != tinyWhite {
		t.Fatal("unexpected colors before barrier")
	}
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if c.tinyColorOf(handleOf(child)) != tinyGray || c.tinyColorOf(handleOf(parent)) != tinyGray {
		t.Fatal("tiny write barrier did not gray child and re-gray parent")
	}
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if c.entry(child).space == spaceFree {
		t.Fatal("barrier-protected child reclaimed")
	}
}

func TestTinyAllocationFailureCollectsWithRootsAndMinorAlias(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 64, TinyBlockBytes: 16})
	rooted, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.NewStructDefault(0)
	root := Root(rooted)
	if _, err := c.NewStructDefaultWithRoots(0, Slots{&root}); err != nil {
		t.Fatalf("allocation did not collect dead object and retry: %v", err)
	}
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().MinorCollections != 1 || c.Stats().FullCollections == 0 {
		t.Fatalf("unexpected stats: %+v", c.Stats())
	}
}

func TestTinyConfigValidationAndClose(t *testing.T) {
	if _, err := NewCollector(Config{Policy: PolicyTiny, TinyHeapBytes: 128, TinyBlockBytes: 12}, testTypes(t)); err == nil {
		t.Fatal("expected non-power-of-two block size rejection")
	}
	c := newTinyTestCollector(t, Config{})
	c.Close()
	if c.tiny.mem != nil || c.handles != nil {
		t.Fatal("tiny close did not release slices")
	}
}
