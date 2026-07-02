package gc

import "testing"

func newTinyTestCollector(t *testing.T, cfg Config) *Collector {
	t.Helper()
	cfg.Profile = ProfileTiny
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

func TestTinyHugeRoundedAllocationDoesNotConsumeZeroBlocks(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 128, TinyBlockBytes: 16})
	before := c.tiny.blocks[0]
	if off, span, err := c.tiny.alloc(^uint32(0) - 7); err == nil {
		t.Fatalf("huge rounded allocation succeeded: off=%d span=%d", off, span)
	}
	if c.tiny.freeHead != 0 || c.tiny.blocks[0] != before {
		t.Fatalf("failed huge allocation corrupted free span: head=%d before=%+v after=%+v", c.tiny.freeHead, before, c.tiny.blocks[0])
	}
}

func TestTinyHugeArrayLengthRejectedWithoutMetadataCorruption(t *testing.T) {
	i8, err := NewArrayDesc(4, StorageI8)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, VerifyAfterCollect: true}, append(testTypes(t), i8))
	length := ^uint32(0) - HeaderSize - 7 // ArraySize rounds this to the largest 8-aligned uint32 size.
	if _, err := c.NewArrayDefault(4, length); err == nil {
		t.Fatal("huge tiny array allocation succeeded")
	}
	if len(c.handles) != 1 || c.tiny.freeHead != 0 || c.tiny.blocks[0].used || c.tiny.blocks[0].size != 8 {
		t.Fatalf("failed huge array allocation corrupted metadata: handles=%d head=%d span=%+v", len(c.handles), c.tiny.freeHead, c.tiny.blocks[0])
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
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

func TestTinyRootRemarkSeesChangedRoot(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 256, TinyBlockBytes: 16})
	a, _ := c.NewStructDefault(0)
	b, _ := c.NewStructDefault(0)
	root := Root(a)
	if err := c.Step(Slots{&root}); err != nil { // idle -> mark, A gray.
		t.Fatal(err)
	}
	if err := c.Step(Slots{&root}); err != nil { // scan A black.
		t.Fatal(err)
	}
	root = Root(b) // frame/local root store: no object barrier.
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if c.entry(b).space == spaceFree {
		t.Fatal("changed root was not remarked before sweep")
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}

func TestTinyRefArrayWritesSkipThroughputCardMetadata(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 512, TinyBlockBytes: 16})
	arr, err := c.NewArrayDefault(3, 4)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(arr, 1, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if c.CardCount() != 0 || c.RememberedCount() != 0 {
		t.Fatalf("tiny ArraySet retained throughput metadata: remembered=%d cards=%d", c.RememberedCount(), c.CardCount())
	}
	c.CardMarkArray(arr, 2)
	c.BulkWriteBarrier(arr, 0, 4)
	if c.CardCount() != 0 || c.RememberedCount() != 0 {
		t.Fatalf("tiny bulk/card barriers retained throughput metadata: remembered=%d cards=%d", c.RememberedCount(), c.CardCount())
	}
}

func TestTinyActiveMarkArrayInitializerKeepsWhiteChild(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 512, TinyBlockBytes: 16})
	anchor, _ := c.NewStructDefault(0)
	child, _ := c.NewStructDefault(0)
	root := Root(anchor)
	if err := c.Step(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if err := c.Step(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	arr, err := c.NewArrayWithRoots(3, 1, RefValue(child), Slots{&root})
	if err != nil {
		t.Fatal(err)
	}
	root = Root(arr)
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if c.entry(child).space == spaceFree {
		t.Fatal("white child from array initializer was reclaimed")
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}

func TestTinyBarrierAvoidsDuplicateGrayPushes(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 512, TinyBlockBytes: 16})
	parent, _ := c.NewStructDefault(1)
	child, _ := c.NewStructDefault(0)
	root := Root(parent)
	_ = c.Step(Slots{&root})
	_ = c.Step(Slots{&root})
	for i := 0; i < 10; i++ {
		if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(c.tinyGC.grayStack); got != 2 {
		t.Fatalf("gray stack duplicates = %d, want child+parent only", got)
	}
}

func FuzzTinyCollectorOperations(f *testing.F) {
	// Seeds pin the stateful Tiny paths that targeted tests exercise separately:
	// incremental object and array barriers, root mutation during remark, global
	// and table roots, and deterministic failed allocation cleanup.
	f.Add([]byte{14, 0, 0, 17, 0, 0})
	f.Add([]byte{15, 0, 0, 17, 0, 0})
	f.Add([]byte{1, 0, 0, 6, 0, 0, 1, 0, 0, 13, 0, 1, 17, 0, 0})
	f.Add([]byte{1, 0, 0, 10, 0, 0, 1, 0, 0, 11, 1, 0, 9, 0, 0, 17, 0, 0})
	f.Add([]byte{1, 0, 0, 6, 0, 0, 1, 0, 0, 6, 1, 0, 12, 0, 0, 16, 0, 0, 17, 0, 0})
	f.Add([]byte("\xd500000200900b00b00"))
	f.Add([]byte("000000000700b00b00b00A00000"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 192 {
			data = data[:192]
		}
		cfg := Config{TinyHeapBytes: 512, TinyBlockBytes: 16, VerifyAfterCollect: true}
		if len(data) != 0 && data[0]&0x80 != 0 {
			cfg.TinyStepEveryAlloc = true
			cfg.TinyStepBudget = 1 + uint32(data[0]&3)
		}
		c := newTinyTestCollector(t, cfg)

		var refs []Ref
		var roots []Root
		globalSlot := c.NewGlobalSlot(Null())
		tableSlot := c.NewTableSlot(Null())
		rootSet := func() Slots { return tinyFuzzSlots(roots) }

		for pc := 0; pc+2 < len(data); pc += 3 {
			op, a, b := data[pc]%18, data[pc+1], data[pc+2]
			slots := func() Slots { return rootSet() }
			switch op {
			case 0:
				if r, err := c.NewStructDefaultWithRoots(0, slots()); err == nil {
					refs = append(refs, r)
				}
			case 1:
				if r, err := c.NewStructDefaultWithRoots(1, slots()); err == nil {
					refs = append(refs, r)
				}
			case 2:
				if r, err := c.NewArrayDefaultWithRoots(3, uint32(a%8)+1, slots()); err == nil {
					refs = append(refs, r)
				}
			case 3:
				if r, err := c.NewArrayDefaultWithRoots(2, uint32(a%8)+1, slots()); err == nil {
					refs = append(refs, r)
				}
			case 4:
				parent, child, ok := fuzzPickTwo(c, refs, a, b)
				if ok {
					_ = c.StructSet(parent, uint32(b)%4, RefValue(child))
				}
			case 5:
				parent, child, ok := fuzzPickTwo(c, refs, a, b)
				if ok {
					if ln, err := c.ArrayLen(parent); err == nil && ln != 0 {
						_ = c.ArraySet(parent, uint32(a+b)%ln, RefValue(child))
					}
				}
			case 6:
				if r, ok := fuzzPick(c, refs, a); ok {
					if len(roots) == 0 || (len(roots) < 16 && b&1 == 0) {
						roots = append(roots, Root(r))
					} else {
						roots[int(b)%len(roots)] = Root(r)
					}
				}
			case 7:
				if len(roots) != 0 {
					idx := int(a) % len(roots)
					roots = append(roots[:idx], roots[idx+1:]...)
				}
			case 8:
				if err := c.Step(slots()); err != nil {
					t.Fatal(err)
				}
			case 9:
				if err := c.CollectFull(slots()); err != nil {
					t.Fatal(err)
				}
			case 10:
				if r, ok := fuzzPick(c, refs, a); ok {
					_ = c.SetGlobalSlot(globalSlot, r)
				} else {
					_ = c.SetGlobalSlot(globalSlot, Null())
				}
			case 11:
				if r, ok := fuzzPick(c, refs, a); ok {
					_ = c.SetTableSlot(tableSlot, r)
				} else {
					_ = c.SetTableSlot(tableSlot, Null())
				}
			case 12:
				_, _ = c.NewArrayDefaultWithRoots(3, 1<<20, slots())
			case 13:
				tinyFuzzMutateRootDuringRemark(t, c, &roots, refs, a, b)
			case 14:
				tinyFuzzBarrierDance(t, c, roots, false)
			case 15:
				tinyFuzzBarrierDance(t, c, roots, true)
			case 16:
				if err := c.CollectMinor(slots()); err != nil {
					t.Fatal(err)
				}
			case 17:
				if err := c.Verify(slots()); err != nil {
					t.Fatal(err)
				}
			}
			refs = pruneLiveRefs(c, refs)
			roots = tinyFuzzPruneRoots(c, roots)
			if err := c.Verify(slots()); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func tinyFuzzSlots(roots []Root) Slots {
	slots := make(Slots, 0, len(roots))
	for i := range roots {
		slots = append(slots, &roots[i])
	}
	return slots
}

func tinyFuzzPruneRoots(c *Collector, roots []Root) []Root {
	out := roots[:0]
	for _, r := range roots {
		ref := Ref(r)
		if ref.IsObj() && validRootRef(c, ref) {
			out = append(out, r)
		}
	}
	return out
}

func tinyFuzzDrain(t *testing.T, c *Collector, roots RootSet) {
	t.Helper()
	limit := len(c.handles)*4 + 32
	for c.tinyGC.state != tinyIdle && limit > 0 {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
		limit--
	}
	if c.tinyGC.state != tinyIdle {
		t.Fatalf("tiny GC did not finish within bounded fuzz drain: state=%d handles=%d", c.tinyGC.state, len(c.handles))
	}
}

func tinyFuzzMutateRootDuringRemark(t *testing.T, c *Collector, roots *[]Root, refs []Ref, keepIdx, targetIdx byte) {
	t.Helper()
	keep, ok := fuzzPick(c, refs, keepIdx)
	if !ok {
		return
	}
	target, ok := fuzzPick(c, refs, targetIdx)
	if !ok {
		return
	}
	if len(*roots) == 0 {
		*roots = append(*roots, Root(keep))
	} else {
		(*roots)[0] = Root(keep)
	}
	rootSet := func() Slots { return tinyFuzzSlots(*roots) }
	if c.tinyGC.state == tinyIdle {
		if err := c.Step(rootSet()); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < len(c.handles)*2+8 && c.tinyGC.state != tinyRemark && c.tinyGC.state != tinyIdle; i++ {
		if err := c.Step(rootSet()); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyGC.state != tinyRemark {
		return
	}
	(*roots)[0] = Root(target)
	tinyFuzzDrain(t, c, rootSet())
	if !validRootRef(c, target) {
		t.Fatalf("root mutated during remark was not preserved: %v", target)
	}
}

func tinyFuzzBarrierDance(t *testing.T, c *Collector, baseRoots []Root, array bool) {
	t.Helper()
	tinyFuzzDrain(t, c, tinyFuzzSlots(baseRoots))
	var (
		parent Ref
		err    error
	)
	if array {
		parent, err = c.NewArrayDefaultWithRoots(3, 2, tinyFuzzSlots(baseRoots))
	} else {
		parent, err = c.NewStructDefaultWithRoots(1, tinyFuzzSlots(baseRoots))
	}
	if err != nil {
		return
	}
	danceRoots := append(append([]Root(nil), baseRoots...), Root(parent))
	child, err := c.NewStructDefaultWithRoots(1, tinyFuzzSlots(danceRoots))
	if err != nil {
		return
	}
	if err := c.Step(tinyFuzzSlots(danceRoots)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(c.handles)+4 && c.tinyGC.state == tinyMark && c.tinyColorOf(handleOf(parent)) != tinyBlack; i++ {
		if err := c.Step(tinyFuzzSlots(danceRoots)); err != nil {
			t.Fatal(err)
		}
	}
	if array {
		_ = c.ArraySet(parent, 0, RefValue(child))
	} else {
		_ = c.StructSet(parent, 0, RefValue(child))
	}
	tinyFuzzDrain(t, c, tinyFuzzSlots(danceRoots))
	if !validRootRef(c, child) {
		t.Fatalf("tiny barrier-protected child was reclaimed: array=%v", array)
	}
}

func TestTinyConfigValidationAndClose(t *testing.T) {
	if _, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 12}, testTypes(t)); err == nil {
		t.Fatal("expected non-power-of-two block size rejection")
	}
	c := newTinyTestCollector(t, Config{})
	c.Close()
	if c.tiny.mem != nil || c.handles != nil {
		t.Fatal("tiny close did not release slices")
	}
}
