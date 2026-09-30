package gc

import (
	"fmt"
	"testing"
)

func fillSparseWholeMinor(tb testing.TB, c *Collector, parent Ref, children []Ref) {
	tb.Helper()
	for i := range children {
		child, err := c.NewStructDefault(0)
		if err != nil {
			tb.Fatal(err)
		}
		children[i] = child
		if err = c.ArraySet(parent, uint32(i*128), RefValue(child)); err != nil {
			tb.Fatal(err)
		}
	}
	if len(c.objectCards) != len(children) {
		tb.Fatalf("got%d ranges, want%d", len(c.objectCards), len(children))
	}
}

func BenchmarkGCSparseCardWholeMinor(b *testing.B) {
	for _, count := range []int{8, 64, 512} {
		b.Run(fmt.Sprintf("ranges=%d", count), func(b *testing.B) {
			c, parent, _ := sparseCardBoundsFixture(b, count)
			root := Root(parent)
			roots := Slots{&root}
			children := make([]Ref, count)
			// Remove the fixture's unused leaf and warm full-collection scratch.
			if err := c.CollectFull(roots); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.StopTimer()
			for i := 0; i < b.N; i++ {
				fillSparseWholeMinor(b, c, parent, children)
				b.StartTimer()
				err := c.CollectMinor(roots)
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				for j, child := range children {
					if !c.validObjectRef(child) {
						b.Fatalf("range%d child lost", j)
					}
					if err = c.ArraySet(parent, uint32(j*128), RefValue(Null())); err != nil {
						b.Fatal(err)
					}
				}
				// Reclaim the promoted children outside timing so old-heap growth does
				// not change the workload across benchmark iterations.
				if err = c.CollectFull(roots); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestSparseCardWholeMinorFixtures(t *testing.T) {
	for _, count := range []int{8, 64, 512} {
		c, parent, _ := sparseCardBoundsFixture(t, count)
		children := make([]Ref, count)
		fillSparseWholeMinor(t, c, parent, children)
		root := Root(parent)
		if err := c.CollectMinor(Slots{&root}); err != nil {
			t.Fatal(err)
		}
		for i, child := range children {
			if !c.validObjectRef(child) {
				t.Fatalf("range%d child lost", i)
			}
		}
	}
}
