package gc

import (
	"errors"
	"slices"
	"testing"
)

type tinySecondWalkFailure struct {
	root  Root
	walks int
}

type tinyLateWalkFailure struct {
	root  Root
	walks int
}

type tinyFirstWalkFailure struct {
	root  Root
	walks int
}

func (r *tinyFirstWalkFailure) RangeRoots(fn func(RootSlot) bool) { fn(&r.root) }

func (r *tinyFirstWalkFailure) RangeRootRefs(sink RootRefSink) bool {
	r.walks++
	// The count walk sees a root, then the callback reports incomplete input.
	if !sink.VisitRootRef(Ref(r.root)) {
		return false
	}
	return false
}

func assertTinyFailedPreflightPreservesCycle(t *testing.T, c *Collector, root Root) {
	t.Helper()
	beforeEpoch := c.tinyGC.markEpoch
	beforeColor := slices.Clone(c.tinyGC.color)
	beforeColorCap := cap(c.tinyGC.color)
	beforeState := c.tinyGC.state
	beforeRootPhase := c.tinyGC.rootPhase
	beforeStack := slices.Clone(c.tinyGC.grayStack)
	beforeStackCap := cap(c.tinyGC.grayStack)
	beforeScan := c.tinyGC.scan
	beforeSweep := c.tinyGC.sweep
	beforeSweepLimit := c.tinyGC.sweepLimit

	roots := &tinyFirstWalkFailure{root: root}
	if err := c.CollectFull(roots); err == nil || roots.walks != 1 {
		t.Fatalf("first root walk: err = %v, walks = %d", err, roots.walks)
	}
	if c.tinyGC.markEpoch != beforeEpoch || !slices.Equal(c.tinyGC.color, beforeColor) || cap(c.tinyGC.color) != beforeColorCap ||
		c.tinyGC.state != beforeState || c.tinyGC.rootPhase != beforeRootPhase ||
		!slices.Equal(c.tinyGC.grayStack, beforeStack) || cap(c.tinyGC.grayStack) != beforeStackCap ||
		c.tinyGC.scan != beforeScan || c.tinyGC.sweep != beforeSweep ||
		c.tinyGC.sweepLimit != beforeSweepLimit {
		t.Fatalf("failed preflight changed cycle: epoch %d->%d, state %d->%d, stack %v->%v, scan %+v->%+v, sweep %d/%d->%d/%d, colors equal %v",
			beforeEpoch, c.tinyGC.markEpoch, beforeState, c.tinyGC.state, beforeStack, c.tinyGC.grayStack,
			beforeScan, c.tinyGC.scan, beforeSweep, beforeSweepLimit, c.tinyGC.sweep, c.tinyGC.sweepLimit,
			slices.Equal(c.tinyGC.color, beforeColor))
	}
}

func TestTinyFailedRootCountPreflightPreservesActiveCycle(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("interrupted mark", func(t *testing.T) {
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
		object, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		root := Root(object)
		late := &tinyLateWalkFailure{root: root}
		if err := c.CollectFull(late); err == nil || late.walks != 2 {
			t.Fatalf("mark walk: err = %v, walks = %d", err, late.walks)
		}
		if c.tinyGC.state != tinyMark || len(c.tinyGC.grayStack) == 0 {
			t.Fatal("setup did not leave unfinished marking work")
		}
		assertTinyFailedPreflightPreservesCycle(t, c, root)
	})
	if !tinyIncrementalBuild {
		return
	}
	t.Run("partial scan", func(t *testing.T) {
		refs, err := NewArrayDesc(1, StorageRefNull)
		if err != nil {
			t.Fatal(err)
		}
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 16, TinyBlockBytes: 16}, []TypeDesc{leaf, refs})
		array, err := c.NewArrayDefault(1, tinyStepScanEntries*2)
		if err != nil {
			t.Fatal(err)
		}
		child, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		for i := uint32(0); i < tinyStepScanEntries*2; i++ {
			if err := c.ArraySet(array, i, RefValue(child)); err != nil {
				t.Fatal(err)
			}
		}
		root := Root(array)
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.scan.handle != handleOf(array) || len(c.tinyGC.grayStack) == 0 {
			t.Fatal("setup did not leave a scan cursor and queued child")
		}
		assertTinyFailedPreflightPreservesCycle(t, c, root)
	})
	t.Run("partial sweep", func(t *testing.T) {
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
		var root Root
		for i := 0; i < 130; i++ {
			object, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			if i == 129 {
				root = Root(object)
			}
		}
		for steps := 0; steps < 16 && c.tinyGC.state != tinySweep; steps++ {
			if err := c.Step(Slots{&root}); err != nil {
				t.Fatal(err)
			}
		}
		if c.tinyGC.state != tinySweep {
			t.Fatal("setup did not reach sweep")
		}
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.state != tinySweep || c.tinyGC.sweep <= 1 || c.tinyGC.sweepLimit <= c.tinyGC.sweep {
			t.Fatal("setup did not leave a partial sweep")
		}
		assertTinyFailedPreflightPreservesCycle(t, c, root)
	})
}

func (r *tinyLateWalkFailure) RangeRoots(fn func(RootSlot) bool) { fn(&r.root) }

func (r *tinyLateWalkFailure) RangeRootRefs(sink RootRefSink) bool {
	r.walks++
	if !sink.VisitRootRef(Ref(r.root)) {
		return false
	}
	return r.walks != 2
}

func TestTinyRecoveryFromEveryCompletedEpoch(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start <= int(tinyMarkEpochMask); start++ {
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1024, TinyBlockBytes: 16}, []TypeDesc{leaf, parentType})
		for i := 0; i < start; i++ {
			if err := c.CollectFull(nil); err != nil {
				t.Fatal(err)
			}
		}
		parent, err := c.NewStructDefault(1)
		if err != nil {
			t.Fatal(err)
		}
		fail := func(late bool) {
			t.Helper()
			if late {
				roots := &tinyLateWalkFailure{root: Root(parent)}
				if err := c.CollectFull(roots); err == nil || roots.walks != 2 {
					t.Fatalf("start %d: late failure = %v, walks %d", start, err, roots.walks)
				}
			} else {
				roots := &tinySecondWalkFailure{root: Root(parent)}
				if err := c.CollectFull(roots); err == nil || roots.walks != 2 {
					t.Fatalf("start %d: early failure = %v, walks %d", start, err, roots.walks)
				}
			}
		}
		fail(false)
		child, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
			t.Fatal(err)
		}
		// A later callback fails after it has already visited the parent.
		fail(true)
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
		fail(false)
		root := Root(parent)
		roots := Slots{&root}
		if err := c.CollectFull(roots); err != nil {
			t.Fatalf("start %d: recovery: %v", start, err)
		}
		field, err := c.StructGet(parent, 0)
		if !c.validObjectRef(parent) || !c.validObjectRef(child) || err != nil || field.Ref != child {
			t.Fatalf("start %d: graph lost: parent %v child %v field %v error %v", start, c.validObjectRef(parent), c.validObjectRef(child), field, err)
		}
		if err := c.Verify(roots); err != nil {
			t.Fatalf("start %d: verify: %v", start, err)
		}
		if err := c.CollectFull(roots); err != nil {
			t.Fatalf("start %d: ordinary follow-up: %v", start, err)
		}
	}
}

func (r *tinySecondWalkFailure) RangeRoots(fn func(RootSlot) bool) {
	fn(&r.root)
}

func (r *tinySecondWalkFailure) RangeRootRefs(sink RootRefSink) bool {
	r.walks++
	if r.walks == 2 {
		return false
	}
	return sink.VisitRootRef(Ref(r.root))
}

func TestTinyCompletedWrapThenFailedRestartsKeepReachableChild(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf, parentType})
	for i := 0; i < int(tinyMarkEpochMask); i++ {
		if err := c.CollectFull(nil); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyGC.markEpoch != tinyMarkEpochMask || c.tinyGC.state != tinyIdle {
		t.Fatalf("setup epoch/state = %d/%d", c.tinyGC.markEpoch, c.tinyGC.state)
	}
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	if c.tinyColorOf(handleOf(parent)) != tinyBlack {
		t.Fatal("idle parent was not black")
	}
	failStart := func() {
		t.Helper()
		roots := &tinySecondWalkFailure{root: Root(parent)}
		if err := c.CollectFull(roots); err == nil || roots.walks != 2 {
			t.Fatalf("second root walk: err = %v, walks = %d", err, roots.walks)
		}
	}
	failStart() // epoch 0, after a completed epoch-127 cycle
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	garbage, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < int(tinyMarkEpochMask); i++ {
		failStart()
	}
	root := Root(parent)
	roots := Slots{&root}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(parent) || !c.validObjectRef(child) {
		t.Fatalf("reachable graph lost: parent=%v child=%v epoch=%d", c.validObjectRef(parent), c.validObjectRef(child), c.tinyGC.markEpoch)
	}
	field, err := c.StructGet(parent, 0)
	if err != nil || field.Ref != child {
		t.Fatalf("parent field = %v, %v; want %v", field, err, child)
	}
	if c.validObjectRef(garbage) {
		t.Fatal("unreachable garbage retained")
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
	reused, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.validObjectRef(reused) || !c.validObjectRef(child) {
		t.Fatal("later collection lost child or retained garbage")
	}
	field, err = c.StructGet(parent, 0)
	if err != nil || field.Ref != child {
		t.Fatalf("later parent field = %v, %v; want %v", field, err, child)
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

func TestTinyFailedRestartsDoNotAliasWrappedEpoch(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf, parentType})
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.markEpoch != 1 {
		t.Fatalf("initial mark epoch = %d, want 1", c.tinyGC.markEpoch)
	}
	failSecondWalk := func() {
		t.Helper()
		roots := &tinySecondWalkFailure{root: Root(parent)}
		if err := c.CollectFull(roots); err == nil || roots.walks != 2 {
			t.Fatalf("second root walk: err = %v, walks = %d", err, roots.walks)
		}
	}
	failSecondWalk()
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	garbage, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := uint8(2); i < tinyMarkEpochMask; i++ {
		failSecondWalk()
	}
	root := Root(parent)
	roots := Slots{&root}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(parent) || !c.validObjectRef(child) {
		t.Fatal("wrapped cycle lost a live parent or child")
	}
	if c.validObjectRef(garbage) {
		t.Fatal("wrapped cycle retained an unrooted object")
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
	reused, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if handleOf(reused) != handleOf(garbage) {
		t.Fatalf("new handle = %d, want reused handle %d", handleOf(reused), handleOf(garbage))
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.validObjectRef(reused) || !c.validObjectRef(child) {
		t.Fatal("next cycle lost the child or retained the reused handle")
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

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
		wantEpoch := uint8(cycle) & tinyMarkEpochMask
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
	// synchronous restart must select 1 without aliasing either old population.
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
	keepRoot := Root(keep)
	if err := c.CollectFull(Slots{&keepRoot}); err != nil {
		t.Fatal(err)
	}
	if want := uint8(1); c.tinyGC.markEpoch != want {
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
	if c.tinyGC.markEpoch != 1 {
		t.Fatalf("restart epoch = %d, want wrapped epoch 1", c.tinyGC.markEpoch)
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
