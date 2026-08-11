package gc

import "testing"

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
