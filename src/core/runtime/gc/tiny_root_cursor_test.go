package gc

import (
	"strings"
	"testing"
)

func TestTinyPersistentRootEnumerationIsResumable(t *testing.T) {
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

func TestTinyRejectsUnboundedTransientRootWalk(t *testing.T) {
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
	if err := c.Step(roots); err == nil || !strings.Contains(err.Error(), "transient root") {
		t.Fatalf("Step error = %v, want bounded transient-root rejection", err)
	}

	c2 := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	root := Root(Null())
	if err := c2.Step(fallbackTinyRoots{root}); err == nil || !strings.Contains(err.Error(), "bounded direct enumeration") {
		t.Fatalf("fallback Step error = %v, want bounded direct-enumeration rejection", err)
	}
}
