package gc

import (
	"strings"
	"testing"
)

func TestTinyPersistentRootEnumerationIsResumable(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf})
	const roots = 600
	objects := make([]Ref, roots)
	for i := range objects {
		objects[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		c.NewGlobalSlot(objects[i])
	}

	if err := c.Step(nil); err != nil {
		t.Fatal(err)
	}
	if got := c.tinyColorOf(handleOf(objects[0])); got == tinyWhite {
		t.Fatal("first persistent-root chunk was not marked")
	}
	if got := c.tinyColorOf(handleOf(objects[roots-1])); got != tinyWhite {
		t.Fatalf("last persistent root color after one Step = %v, want white", got)
	}

	for c.tinyGC.state != tinyIdle {
		if err := c.Step(nil); err != nil {
			t.Fatal(err)
		}
	}
	for i, object := range objects {
		if !c.validObjectRef(object) {
			t.Fatalf("persistent root %d was reclaimed", i)
		}
	}
}

func TestTinyPersistentRootMutationAroundCursor(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf})
	const rootCount = 600
	objects := make([]Ref, rootCount)
	for i := range objects {
		objects[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		c.NewGlobalSlot(objects[i])
	}
	moved := Root(Null())
	if err := c.Step(Slots{&moved}); err != nil {
		t.Fatal(err)
	}
	if c.tinyColorOf(handleOf(objects[rootCount-1])) != tinyWhite {
		t.Fatal("test did not leave a persistent root beyond the cursor white")
	}

	moved = Root(objects[rootCount-1])
	if err := c.SetGlobalSlot(rootCount-1, Null()); err != nil {
		t.Fatal(err)
	}
	if err := c.SetGlobalSlot(0, objects[rootCount-2]); err != nil {
		t.Fatal(err)
	}
	if err := c.SetGlobalSlot(rootCount-2, Null()); err != nil {
		t.Fatal(err)
	}
	appended, err := c.NewCheckedGlobalSlot(objects[rootCount-3])
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetGlobalSlot(rootCount-3, Null()); err != nil {
		t.Fatal(err)
	}

	for c.tinyGC.state != tinyIdle {
		if err := c.Step(Slots{&moved}); err != nil {
			t.Fatal(err)
		}
	}
	for _, object := range []Ref{Ref(moved), objects[rootCount-2], objects[rootCount-3]} {
		if !c.validObjectRef(object) {
			t.Fatalf("root mutation around cursor reclaimed %#x", object)
		}
	}
	if got := c.GlobalSlot(appended); got != objects[rootCount-3] {
		t.Fatalf("appended root = %#x, want %#x", got, objects[rootCount-3])
	}
}

type fallbackTinyRoots []Root

func (r fallbackTinyRoots) RangeRoots(fn func(RootSlot) bool) {
	for i := range r {
		if !fn(&r[i]) {
			return
		}
	}
}

type classifiedOnlyTinyRoots []Root

func (r classifiedOnlyTinyRoots) RangeRoots(fn func(RootSlot) bool) {
	for i := range r {
		if !fn(&r[i]) {
			return
		}
	}
}

func (r classifiedOnlyTinyRoots) RangeClassifiedRootRefs(sink ClassifiedRootRefSink) bool {
	for i := range r {
		if !sink.VisitClassifiedRootRef(RootSnapshotTemporary, Ref(r[i])) {
			return false
		}
	}
	return true
}

func TestTinyClassifiedTransientRootsCountOnceWithoutTelemetry(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	newCollector := func(t *testing.T) (*Collector, Ref) {
		t.Helper()
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, []TypeDesc{leaf})
		object, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		return c, object
	}

	for _, count := range []int{1024, 4096} {
		c, object := newCollector(t)
		roots := make(classifiedOnlyTinyRoots, count)
		for i := range roots {
			roots[i] = Root(object)
		}
		if err := c.Step(roots); err != nil {
			t.Fatalf("Step with %d classified roots: %v", len(roots), err)
		}
	}
}

func TestTinyRejectsTransientRootsWithoutDirectEnumeration(t *testing.T) {
	requireTinyIncrementalBuild(t)
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, []TypeDesc{leaf})
	roots := make(RefSliceRoots, 1025)
	for i := range roots {
		roots[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Step(roots); err != nil {
		t.Fatalf("direct transient roots above the old limit: %v", err)
	}

	c2 := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	root := Root(Null())
	if err := c2.Step(fallbackTinyRoots{root}); err == nil || !strings.Contains(err.Error(), "bounded direct enumeration") {
		t.Fatalf("fallback Step error = %v, want bounded direct-enumeration rejection", err)
	}
	c3 := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	nested := RootGroups{{Class: RootSnapshotTemporary, Roots: fallbackTinyRoots{root}}}
	if err := c3.Step(nested); err == nil || !strings.Contains(err.Error(), "bounded direct enumeration") {
		t.Fatalf("nested fallback Step error = %v, want bounded direct-enumeration rejection", err)
	}
	c4 := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	if err := c4.Step(&nested); err == nil || !strings.Contains(err.Error(), "bounded direct enumeration") {
		t.Fatalf("pointer nested fallback Step error = %v, want bounded direct-enumeration rejection", err)
	}
}

func TestClassifiedFallbackRootsRemainVisibleToThroughputTelemetry(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Telemetry: new(Telemetry), ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}, []TypeDesc{leaf})
	object, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(object)
	roots := ClassifiedRoots{Class: RootSnapshotTemporary, Roots: fallbackTinyRoots{root}}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(object) {
		t.Fatal("classified fallback root was skipped by Throughput collection")
	}
}
