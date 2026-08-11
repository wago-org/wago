package gc

import "testing"

func TestTinyEpochAdvanceMakesOldMarksWhiteWithoutRewrite(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	rooted, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	unrooted, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if c.tinyColorOf(handleOf(rooted)) != tinyBlack || c.tinyColorOf(handleOf(unrooted)) != tinyBlack {
		t.Fatal("idle allocations are not black in the current epoch")
	}
	oldEpoch := c.tinyGC.markEpoch
	unrootedState := c.tinyGC.color[handleOf(unrooted)]
	root := Root(rooted)
	if err := c.Step(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if want := (oldEpoch + 1) & tinyMarkEpochMask; c.tinyGC.markEpoch != want {
		t.Fatalf("mark epoch = %d, want %d", c.tinyGC.markEpoch, want)
	}
	if got := c.tinyGC.color[handleOf(unrooted)]; got != unrootedState {
		t.Fatalf("cycle start rewrote unrooted mark state from %#x to %#x", unrootedState, got)
	}
	if got := c.tinyColorOf(handleOf(unrooted)); got != tinyWhite {
		t.Fatalf("old-epoch object color = %v, want white", got)
	}
	if got := c.tinyColorOf(handleOf(rooted)); got != tinyGray {
		t.Fatalf("root color = %v, want gray", got)
	}
}

func TestTinySweepRetainsSurvivorMarkUntilNextEpoch(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	object, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(object)
	roots := Slots{&root}

	for c.tinyGC.state != tinySweep {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
	}
	h := handleOf(object)
	if got := c.tinyColorOf(h); got != tinyBlack {
		t.Fatalf("survivor color before sweep = %v, want black", got)
	}
	before := c.tinyGC.color[h]
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	if got := c.tinyGC.color[h]; got != before {
		t.Fatalf("sweep rewrote survivor mark state from %v to %v", before, got)
	}
	if got := c.tinyColorOf(h); got != tinyBlack {
		t.Fatalf("survivor color after sweep = %v, want black until the next epoch", got)
	}
}

func TestTinyEpochWrapAndHandleReuse(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf})
	rooted, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(rooted)
	roots := Slots{&root}
	initialEpoch := c.tinyGC.markEpoch
	var reusedHandle uint32
	for cycle := uint32(1); cycle <= 3*uint32(tinyMarkEpochMask+1); cycle++ {
		garbage, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if reusedHandle != 0 && handleOf(garbage) != reusedHandle {
			t.Fatalf("garbage handle = %d, want reused handle %d", handleOf(garbage), reusedHandle)
		}
		reusedHandle = handleOf(garbage)
		if got := c.tinyColorOf(reusedHandle); got != tinyBlack {
			t.Fatalf("cycle %d reused idle handle color = %v, want black", cycle, got)
		}
		if err := c.CollectFull(roots); err != nil {
			t.Fatal(err)
		}
		if c.validObjectRef(garbage) {
			t.Fatalf("cycle %d retained unrooted handle %d", cycle, reusedHandle)
		}
		wantEpoch := (initialEpoch + uint8(cycle)) & tinyMarkEpochMask
		if c.tinyGC.markEpoch != wantEpoch {
			t.Fatalf("cycle %d epoch = %d, want %d", cycle, c.tinyGC.markEpoch, wantEpoch)
		}
		if got := c.tinyColorOf(handleOf(rooted)); got != tinyBlack {
			t.Fatalf("cycle %d rooted survivor color = %v, want black", cycle, got)
		}
	}
}

func TestTinyCollectFullRestartsPartialScanWithFreshEpoch(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf, refs})
	// Start one epoch before wrap so the incremental cycle uses 127 and the
	// synchronous restart must select 0 without aliasing either old population.
	c.tinyGC.markEpoch = tinyMarkEpochMask - 1
	c.tinyGC.color[0] = tinyEncodeMarkState(c.tinyGC.markEpoch, tinyWhite)
	partial, err := c.NewArrayDefault(1, tinyStepScanEntries*2)
	if err != nil {
		t.Fatal(err)
	}
	keep, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	drop, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	partialRoot := Root(partial)
	if err := c.Step(Slots{&partialRoot}); err != nil {
		t.Fatal(err)
	}
	if err := c.Step(Slots{&partialRoot}); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.scan.handle != handleOf(partial) || c.tinyColorOf(handleOf(partial)) != tinyGray {
		t.Fatal("test did not establish a partial gray scan")
	}
	if c.tinyColorOf(handleOf(keep)) != tinyWhite || c.tinyColorOf(handleOf(drop)) != tinyWhite {
		t.Fatal("unvisited objects are not white in the active epoch")
	}
	activeEpoch := c.tinyGC.markEpoch
	keepRoot := Root(keep)
	if err := c.CollectFull(Slots{&keepRoot}); err != nil {
		t.Fatal(err)
	}
	if want := (activeEpoch + 1) & tinyMarkEpochMask; c.tinyGC.markEpoch != want {
		t.Fatalf("restart epoch = %d, want %d", c.tinyGC.markEpoch, want)
	}
	if !c.validObjectRef(keep) || c.tinyColorOf(handleOf(keep)) != tinyBlack {
		t.Fatal("fresh restart did not retain the new exact root")
	}
	if c.validObjectRef(partial) || c.validObjectRef(drop) {
		t.Fatal("fresh restart aliased old white/current marks and retained garbage")
	}
}

func TestTinyAllocationsPublishCurrentEpochState(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 16, TinyBlockBytes: 16}, []TypeDesc{leaf, refs})
	assertCurrent := func(ref Ref, color tinyColor) {
		t.Helper()
		h := handleOf(ref)
		state := c.tinyGC.color[h]
		if uint8(state)&tinyMarkEpochMask != c.tinyGC.markEpoch {
			t.Fatalf("handle %d state %#x is not in current epoch %d", h, state, c.tinyGC.markEpoch)
		}
		if got := c.tinyColorOf(h); got != color {
			t.Fatalf("handle %d color = %v, want %v", h, got, color)
		}
	}

	rooted, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrent(rooted, tinyBlack)
	root := Root(rooted)
	roots := Slots{&root}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	markRefs, err := c.NewArrayDefault(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrent(markRefs, tinyGray)
	markLeaf, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrent(markLeaf, tinyBlack)

	for c.tinyGC.state != tinyRemark {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
	}
	remarkRefs, err := c.NewArrayDefault(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrent(remarkRefs, tinyGray)
	for c.tinyGC.state != tinySweep {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
	}
	sweepEpoch := c.tinyGC.markEpoch
	afterSweepRequest, err := c.NewStructDefaultWithRoots(0, roots)
	if err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.state != tinyIdle || c.tinyGC.markEpoch != sweepEpoch {
		t.Fatalf("allocation request did not finish sweep in the same epoch: state=%v epoch=%d want=%d", c.tinyGC.state, c.tinyGC.markEpoch, sweepEpoch)
	}
	assertCurrent(afterSweepRequest, tinyBlack)
}

func TestTinyVerifyRejectsInvalidEpochMetadata(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("epoch", func(t *testing.T) {
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
		c.tinyGC.markEpoch = tinyMarkEpochMask + 1
		if err := c.Verify(nil); err == nil {
			t.Fatal("Verify accepted a mark epoch outside the encoded range")
		}
	})
	t.Run("truncated marks", func(t *testing.T) {
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
		c.tinyGC.color = c.tinyGC.color[:1]
		if err := c.Verify(nil); err == nil {
			t.Fatal("Verify accepted truncated mark metadata")
		}
	})
}
