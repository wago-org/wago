package gc

import (
	"fmt"
	"strings"
	"testing"
)

type oneShotDirectRoots struct {
	ref   Ref
	calls int
}

func (r *oneShotDirectRoots) RangeRoots(func(RootSlot) bool) {}

func (r *oneShotDirectRoots) RangeRootRefs(sink RootRefSink) bool {
	r.calls++
	if r.calls == 1 {
		return sink.VisitRootRef(r.ref)
	}
	return true
}

func TestTinyKeepsOneShotDirectRoot(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		profile Profile
	}{{"tiny", ProfileTiny}, {"throughput", ProfileThroughput}} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCollector(Config{Profile: tc.profile, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			object, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			roots := &oneShotDirectRoots{ref: object}
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if !c.validObjectRef(object) {
				t.Fatalf("profile %d freed one-shot root after %d walks", tc.profile, roots.calls)
			}
		})
	}
}

type oneShotClassifiedRoots struct {
	ref   Ref
	calls int
}

func (r *oneShotClassifiedRoots) RangeRoots(func(RootSlot) bool) {}
func (r *oneShotClassifiedRoots) RangeClassifiedRootRefs(sink ClassifiedRootRefSink) bool {
	r.calls++
	if r.calls == 1 {
		return sink.VisitClassifiedRootRef(RootSnapshotTemporary, r.ref)
	}
	return true
}

func TestTinyOneShotRootsKeepGraphAndReleaseNextCycle(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	for _, measured := range []bool{false, true} {
		for _, form := range []string{"direct", "classified", "composite"} {
			t.Run(fmt.Sprintf("%s/telemetry=%v", form, measured), func(t *testing.T) {
				cfg := Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}
				if measured {
					cfg.Telemetry = new(Telemetry)
				}
				c := newTestCollectorWithTypes(t, cfg, []TypeDesc{leaf, parentType})
				parent, err := c.NewStructDefault(1)
				if err != nil {
					t.Fatal(err)
				}
				child, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				garbage, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
					t.Fatal(err)
				}
				direct := &oneShotDirectRoots{ref: parent}
				classified := &oneShotClassifiedRoots{ref: parent}
				var roots RootSet = direct
				calls := &direct.calls
				if form == "classified" {
					roots, calls = classified, &classified.calls
				} else if form == "composite" {
					roots = RootGroups{{Class: RootSnapshotTemporary, Roots: combineRootSets(direct, EmptyRoots{})}}
				}
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				wantWalks := 1
				if tinyIncrementalBuild {
					wantWalks = 2
				} // Initial roots, then remark.
				if *calls != wantWalks {
					t.Fatalf("root walks = %d, want %d", *calls, wantWalks)
				}
				field, err := c.StructGet(parent, 0)
				if !c.validObjectRef(parent) || !c.validObjectRef(child) || c.validObjectRef(garbage) || err != nil || field.Ref != child {
					t.Fatalf("one-shot graph lost or garbage retained: field=%v error=%v", field, err)
				}
				if err := c.Verify(RefSliceRoots{parent}); err != nil {
					t.Fatal(err)
				}
				if snapshot, ok := c.TelemetrySnapshot(); ok {
					want := RootTelemetry{NativeFrames: 1}
					if form != "direct" {
						want = RootTelemetry{SnapshotTemporaries: 1}
					}
					got := snapshot.Full.Roots
					if got.NativeFrames != want.NativeFrames || got.SnapshotTemporaries != want.SnapshotTemporaries ||
						got.Globals != 0 || got.Tables != 0 || got.PublicTokens != 0 || got.ForeignInstances != 0 {
						t.Fatalf("root accounting = %+v, want counts %+v", got, want)
					}
				}
				if err := c.CollectFull(nil); err != nil {
					t.Fatal(err)
				}
				if c.validObjectRef(parent) || c.validObjectRef(child) {
					t.Fatal("staging retained roots into the next cycle")
				}
				if err := c.Verify(nil); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestTinyTransientRootBufferReuse(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, []TypeDesc{leaf})
	refs := make(RefSliceRoots, 4096)
	for i := range refs {
		refs[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	var roots RootSet = refs
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if !c.validObjectRef(ref) {
			t.Fatal("large root set lost an object")
		}
	}
	capacity := cap(c.markStack)
	if capacity < len(refs) || len(c.markStack) != 0 {
		t.Fatal("staging buffer was not retained empty for reuse")
	}
	if allocs := testing.AllocsPerRun(20, func() {
		if err := c.CollectFull(roots); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Fatalf("warm collection allocations = %v, want zero", allocs)
	}
	if cap(c.markStack) != capacity {
		t.Fatal("warm collection grew the staging buffer")
	}
	// Reuse a much smaller root set; stale buffer entries must not retain objects.
	if err := c.CollectFull(RefSliceRoots{refs[0], refs[0], Null(), I31New(7), Ref(0xfffffffe)}); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(refs[0]) {
		t.Fatal("small root set lost its object")
	}
	for _, ref := range refs[1:] {
		if c.validObjectRef(ref) {
			t.Fatal("stale staging entry retained an object")
		}
	}
	if err := c.Verify(RefSliceRoots{refs[0]}); err != nil {
		t.Fatal(err)
	}
}

func TestTinyRejectedNestedRootsStopEnumeration(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	object, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	failed := &tinyFailingRoots{root: Root(object), afterRoot: true}
	later := &oneShotDirectRoots{ref: object}
	groups := RootGroups{
		{Class: RootNativeFrame, Roots: &ClassifiedRoots{Class: RootSnapshotTemporary, Roots: failed}},
		{Class: RootNativeFrame, Roots: later},
	}
	if err := c.CollectFull(&groups); err == nil || !strings.Contains(err.Error(), "enumeration stopped unexpectedly") {
		t.Fatalf("nested incomplete walk: %v", err)
	}
	if failed.walks != 1 || later.calls != 0 {
		t.Fatalf("walks after rejection: failed=%d later=%d, want 1/0", failed.walks, later.calls)
	}
	if c.tinyGC.state != tinyIdle || len(c.markStack) != 0 || c.rootMarkMode != 0 {
		t.Fatal("nested rejection left an active cycle or staged roots")
	}
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if c.validObjectRef(object) {
		t.Fatal("nested rejection retained its partial root")
	}
}

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
