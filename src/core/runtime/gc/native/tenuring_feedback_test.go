package gc

import "testing"

func TestTenuringFeedbackDoesNotCountPromotionAsSurvivorOccupancy(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, SurvivorBytes: 512, VerifyAfterCollect: true})
	c.tenuringThreshold = 1
	roots := make(RefSliceRoots, 32)
	for i := range roots {
		var err error
		roots[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	before := c.Stats()
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	after := c.Stats()
	if c.survivorBump != 0 || after.PromotedBytes <= before.PromotedBytes {
		t.Fatal("fixture did not promote every survivor")
	}
	if c.tenuringThreshold != 2 {
		t.Fatalf("empty survivor space after promotion kept threshold %d, want recovery to 2", c.tenuringThreshold)
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

func TestTenuringFeedbackCountsOccupiedSurvivorSpace(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, SurvivorBytes: 512, VerifyAfterCollect: true})
	c.tenuringThreshold = 3
	roots := make(RefSliceRoots, 16)
	for i := range roots {
		var err error
		roots[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	if c.survivorBump < c.survivorBytes*3/4 {
		t.Fatalf("fixture survivor occupancy %d/%d", c.survivorBump, c.survivorBytes)
	}
	if c.tenuringThreshold != 2 {
		t.Fatalf("threshold=%d, want pressure reduction to 2", c.tenuringThreshold)
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

func TestTenuringFeedbackLargePromotionDoesNotOccupySurvivor(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, SurvivorBytes: 512, LargeObjectBytes: 64, VerifyAfterCollect: true})
	c.tenuringThreshold = 1
	ref, err := c.NewArrayDefault(2, 128)
	if err != nil {
		t.Fatal(err)
	}
	roots := RefSliceRoots{ref}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	if c.survivorBump != 0 || c.entry(ref).young() {
		t.Fatal("large object did not tenure in place")
	}
	if c.tenuringThreshold != 2 {
		t.Fatalf("threshold=%d, want 2", c.tenuringThreshold)
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}
