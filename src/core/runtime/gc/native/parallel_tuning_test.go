//go:build !tinygo && !wago_target_tinygo

package gc

import "testing"

// Exercise both sides of the captured-list optimization without timing: sparse
// survival must still materialize every live handle; dense survival must not.
func TestParallelCaptureDenseAndSparse(t *testing.T) {
	parallelTestProcessors(t)
	leaf, _ := NewStructDesc(0, nil)
	array, _ := NewArrayDesc(1, StorageRefNull)
	c, err := NewCollector(Config{Workers: 4, NurseryBytes: 8 << 20}, []TypeDesc{leaf, array})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const count = 9000
	parent, err := c.NewArrayDefault(1, count)
	if err != nil {
		t.Fatal(err)
	}
	for i := range count {
		r, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err = c.ArraySet(parent, uint32(i), RefValue(r)); err != nil {
			t.Fatal(err)
		}
	}
	roots := RefSliceRoots{parent}
	c.promotionScratch = make([]plannedPromotion, 0, count+100)
	backing := &c.promotionScratch[:cap(c.promotionScratch)][0]
	for _, sparse := range []bool{false, true, true, false} {
		var dead Ref
		if sparse {
			dead, err = c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
		}
		c.clearNurseryMarks()
		c.markNurseryRoots(roots)
		scanned, ok := c.drainParallel(true, true)
		if !ok || scanned != count+1 {
			t.Fatalf("scanned=%d parallel=%v", scanned, ok)
		}
		want := 0
		if sparse {
			want = count + 1
		}
		if len(c.promotionScratch) != want {
			t.Fatalf("sparse=%v captured=%d want=%d", sparse, len(c.promotionScratch), want)
		}
		if &c.promotionScratch[:cap(c.promotionScratch)][0] != backing {
			t.Fatal("capture discarded sufficient retained storage")
		}
		seen := make(map[uint32]bool)
		for _, p := range c.promotionScratch {
			if seen[p.handle] || !c.mark[p.handle] {
				t.Fatal("duplicate or unmarked captured handle")
			}
			seen[p.handle] = true
		}
		if sparse {
			c.free(handleOf(dead))
			c.compactNurseryHandles()
		}
	}
}
