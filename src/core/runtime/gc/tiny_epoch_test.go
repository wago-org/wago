package gc

import (
	"errors"
	"testing"
)

func TestTinyMarkStateDecodingExhaustive(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	object, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	h := handleOf(object)
	for epoch := uint16(0); epoch <= uint16(tinyMarkEpochMask); epoch++ {
		c.tinyGC.markEpoch = uint8(epoch)
		for raw := uint16(0); raw <= 255; raw++ {
			c.tinyGC.color[h] = tinyMarkState(raw)
			want := tinyWhite
			if raw == epoch {
				want = tinyBlack
			} else if raw == epoch|uint16(tinyMarkGrayBit) {
				want = tinyGray
			}
			if got := c.tinyColorOf(h); got != want {
				t.Fatalf("epoch=%d raw=%#x color=%v, want %v", epoch, raw, got, want)
			}
		}
	}
}

func TestTinyEpochAdvanceMakesOldMarksWhiteWithoutRewrite(t *testing.T) {
	requireTinyIncrementalBuild(t)
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
	requireTinyIncrementalBuild(t)
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
	requireTinyIncrementalBuild(t)
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

func TestTinyCollectFullRestartsSweepWithFreshEpoch(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf})
	c.tinyGC.markEpoch = tinyMarkEpochMask - 1
	c.tinyGC.color[0] = tinyEncodeMarkState(c.tinyGC.markEpoch, tinyWhite)
	oldRoot, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < int(tinyStepSweepHandles); i++ {
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	keep, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	drop, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(oldRoot)
	roots := Slots{&root}
	for c.tinyGC.state != tinySweep {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyGC.markEpoch != tinyMarkEpochMask {
		t.Fatalf("sweep epoch = %d, want %d", c.tinyGC.markEpoch, tinyMarkEpochMask)
	}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.state != tinySweep || c.tinyGC.sweep <= 1 {
		t.Fatalf("bounded sweep did not advance partially: state=%d cursor=%d", c.tinyGC.state, c.tinyGC.sweep)
	}
	keepRoot := Root(keep)
	if err := c.CollectFull(Slots{&keepRoot}); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.markEpoch != 0 {
		t.Fatalf("restart epoch = %d, want wrapped epoch 0", c.tinyGC.markEpoch)
	}
	if !c.validObjectRef(keep) || c.validObjectRef(oldRoot) || c.validObjectRef(drop) {
		t.Fatal("sweep restart retained the wrong epoch population")
	}
}

func TestTinyCheckedRootPublicationRejectsUnsafeSweepGraph(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf, parentType})
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < int(tinyStepSweepHandles); i++ {
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	safe, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	global := c.NewGlobalSlot(Null())
	table := c.NewTableSlot(Null())
	for c.tinyGC.state != tinySweep {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Step(nil); err != nil {
		t.Fatal(err)
	}
	if c.validObjectRef(child) || !c.validObjectRef(parent) || c.tinyColorOf(handleOf(parent)) != tinyWhite {
		t.Fatal("test did not establish a white parent with an earlier reclaimed child")
	}
	for _, set := range []struct {
		name string
		fn   func() error
	}{
		{name: "global", fn: func() error { return c.SetGlobalSlot(global, parent) }},
		{name: "table", fn: func() error { return c.SetTableSlot(table, parent) }},
	} {
		t.Run(set.name, func(t *testing.T) {
			if err := set.fn(); !errors.Is(err, errTinyUnsafeSweepRoot) {
				t.Fatalf("error = %v, want %v", err, errTinyUnsafeSweepRoot)
			}
		})
	}
	beforeGlobals, beforeTables := len(c.globalSlots), len(c.tableSlots)
	if _, err := c.NewCheckedGlobalSlot(parent); !errors.Is(err, errTinyUnsafeSweepRoot) {
		t.Fatalf("new global error = %v, want %v", err, errTinyUnsafeSweepRoot)
	}
	if _, err := c.NewCheckedTableSlot(parent); !errors.Is(err, errTinyUnsafeSweepRoot) {
		t.Fatalf("new table error = %v, want %v", err, errTinyUnsafeSweepRoot)
	}
	if len(c.globalSlots) != beforeGlobals || len(c.tableSlots) != beforeTables || !c.GlobalSlot(global).IsNull() || !c.TableSlot(table).IsNull() {
		t.Fatal("rejected sweep publication mutated persistent roots")
	}
	if err := c.SetGlobalSlot(global, safe); err != nil {
		t.Fatalf("pointer-free sweep publication failed: %v", err)
	}
	if c.tinyGC.state != tinyMark || c.tinyColorOf(handleOf(safe)) != tinyGray {
		t.Fatal("pointer-free sweep publication was not queued before sweep resumed")
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	if c.validObjectRef(parent) || !c.validObjectRef(safe) {
		t.Fatal("sweep publication retained the unsafe graph or lost the safe object")
	}
}

func TestTinyCheckedRootPublicationAllowsMarkedSweepGraph(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf, parentType})
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	for c.tinyGC.state != tinySweep {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyColorOf(handleOf(parent)) != tinyBlack || c.tinyColorOf(handleOf(child)) != tinyBlack {
		t.Fatal("exactly rooted graph was not black on entry to sweep")
	}
	global := c.NewGlobalSlot(Null())
	if err := c.SetGlobalSlot(global, parent); err != nil {
		t.Fatalf("marked sweep graph publication failed: %v", err)
	}
	root = Root(Null())
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if !c.validObjectRef(parent) || !c.validObjectRef(child) {
		t.Fatal("marked sweep graph publication was not retained")
	}
}

func TestTinyAllocationsPublishCurrentEpochState(t *testing.T) {
	requireTinyIncrementalBuild(t)
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
	if c.tinyGC.state != tinySweep || c.tinyGC.markEpoch != sweepEpoch {
		t.Fatalf("allocation request did not remain in bounded sweep: state=%v epoch=%d want=%d", c.tinyGC.state, c.tinyGC.markEpoch, sweepEpoch)
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
	t.Run("sweep limit", func(t *testing.T) {
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
		c.tinyGC.state = tinySweep
		c.tinyGC.sweep = 1
		c.tinyGC.sweepLimit = uint32(len(c.handles) + 1)
		if err := c.Verify(nil); err == nil {
			t.Fatal("Verify accepted a sweep endpoint beyond the handle table")
		}
	})
	t.Run("stale sweep limit", func(t *testing.T) {
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
		c.tinyGC.sweepLimit = 1
		if err := c.Verify(nil); err == nil {
			t.Fatal("Verify accepted an idle collector with a sweep endpoint")
		}
	})
}
