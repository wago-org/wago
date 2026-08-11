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
}
