package gc

import "testing"

func TestTinyRejectedRestartRetainsPreviouslyMarkedRoot(t *testing.T) {
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
	if err := c.Step(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(fallbackTinyRoots{}); err == nil {
		t.Fatal("restart accepted unsupported roots")
	}
	// The rejected request must not discard the earlier cycle's snapshot.
	for steps := 0; c.tinyGC.state != tinyIdle && steps < 32; steps++ {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyGC.state != tinyIdle {
		t.Fatal("cycle did not finish")
	}
	if !c.validObjectRef(object) {
		t.Fatal("rejected restart discarded the active cycle's marked root")
	}
}

func TestTinyFailedRemarkPreservesCycleAndRetries(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	initial, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	late, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	garbage, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Step(RefSliceRoots{initial}); err != nil {
		t.Fatal(err)
	}
	for steps := 0; c.tinyGC.state != tinyRemark && steps < 32; steps++ {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyGC.state != tinyRemark || c.tinyGC.rootPhase != tinyRootsTransient || !c.tinyIsWhite(handleOf(late)) {
		t.Fatal("setup did not reach remark with an unmarked late root")
	}
	assertTinyFailedRootWalkPreservesCycle(t, c, Root(late), false, c.Step)
	assertTinyFailedRootWalkPreservesCycle(t, c, Root(late), true, c.Step)
	// The successful retry must keep a root supplied only during remark.
	roots := &oneShotDirectRoots{ref: late}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	if roots.calls != 1 || c.tinyIsWhite(handleOf(late)) {
		t.Fatal("remark did not mark its one-shot root on the first walk")
	}
	for steps := 0; c.tinyGC.state != tinyIdle && steps < 32; steps++ {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	if c.tinyGC.state != tinyIdle || !c.validObjectRef(initial) || !c.validObjectRef(late) || c.validObjectRef(garbage) {
		t.Fatal("remark recovery lost a live root, retained garbage, or failed to finish")
	}
	if err := c.Verify(RefSliceRoots{initial, late}); err != nil {
		t.Fatal(err)
	}
}

func TestTinyRejectedCompositeRootsDoNotPublishPartialMarks(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	keep, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	drop, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.tinyStartMark(RefSliceRoots{keep}); err != nil {
		t.Fatal(err)
	}
	beforeEpoch := c.tinyGC.markEpoch
	partial := &oneShotDirectRoots{ref: drop}
	roots := RootGroups{
		{Class: RootNativeFrame, Roots: partial},
		{Class: RootSnapshotTemporary, Roots: fallbackTinyRoots{}},
	}
	if err := c.CollectFull(roots); err == nil {
		t.Fatal("unsupported nested roots were accepted")
	}
	if partial.calls != 1 || c.tinyGC.markEpoch != beforeEpoch || !c.tinyIsWhite(handleOf(drop)) || c.tinyIsWhite(handleOf(keep)) || len(c.markStack) != 0 {
		t.Fatal("rejected composite changed the cycle or retained partial staging work")
	}
	if tinyIncrementalBuild {
		for steps := 0; c.tinyGC.state != tinyIdle && steps < 32; steps++ {
			if err := c.Step(nil); err != nil {
				t.Fatal(err)
			}
		}
	} else if err := c.CollectFull(RefSliceRoots{keep}); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.state != tinyIdle || !c.validObjectRef(keep) || c.validObjectRef(drop) {
		t.Fatal("recovery lost the existing root or retained a rejected root")
	}
}

type tinyPanickingRoots struct{ ref Ref }

func (r tinyPanickingRoots) RangeRoots(func(RootSlot) bool) {}
func (r tinyPanickingRoots) RangeRootRefs(sink RootRefSink) bool {
	sink.VisitRootRef(r.ref)
	panic("root enumeration failed")
}

func TestTinyRootStagingResetsAfterPanic(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	object, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	beforeEpoch := c.tinyGC.markEpoch
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("root walk did not panic")
			}
		}()
		_ = c.CollectFull(tinyPanickingRoots{ref: object})
	}()
	if c.rootMarkMode != 0 || len(c.markStack) != 0 || c.tinyGC.state != tinyIdle || c.tinyGC.markEpoch != beforeEpoch {
		t.Fatal("panicking root walk changed the cycle or left its sink active")
	}
	if err := c.Verify(RefSliceRoots{object}); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if c.validObjectRef(object) {
		t.Fatal("staging leaked a root from the panicking walk")
	}
}
