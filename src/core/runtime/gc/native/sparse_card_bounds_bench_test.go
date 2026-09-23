package gc

import (
	"fmt"
	"testing"
)

func sparseCardBoundsFixture(tb testing.TB, count int) (*Collector, Ref, Ref) {
	tb.Helper()
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		tb.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		tb.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 4 << 20, DisableMovingNursery: true}, []TypeDesc{leaf, refs})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(c.Close)
	parent, err := c.NewArrayDefault(1, uint32(count*128+1))
	if err != nil {
		tb.Fatal(err)
	}
	if err = c.ForcePromote(parent); err != nil {
		tb.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		tb.Fatal(err)
	}
	return c, parent, child
}

func BenchmarkGCSparseCardInsert(b *testing.B) {
	for _, count := range []int{1, 8, 64, 512} {
		for _, order := range []string{"ascending", "descending", "random"} {
			b.Run(fmt.Sprintf("cards=%d/%s", count, order), func(b *testing.B) {
				c, parent, child := sparseCardBoundsFixture(b, count)
				indexes := make([]uint32, count)
				for i := range indexes {
					index := i
					if order == "descending" {
						index = count - 1 - i
					}
					if order == "random" {
						index = (i * 73) % count
					}
					indexes[i] = uint32(index * 128)
				}
				// Warm card storage so the timed path reflects lookup rather than growth.
				for _, index := range indexes {
					if err := c.ArraySet(parent, index, RefValue(child)); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					c.clearCardMetadata()
					for _, index := range indexes {
						if err := c.ArraySet(parent, index, RefValue(child)); err != nil {
							b.Fatal(err)
						}
					}
				}
			})
		}
	}
}

func BenchmarkGCSparseCardCollectScan(b *testing.B) {
	for _, count := range []int{1, 8, 64, 512} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			c, parent, child := sparseCardBoundsFixture(b, count)
			for i := 0; i < count; i++ {
				if err := c.ArraySet(parent, uint32(i*128), RefValue(child)); err != nil {
					b.Fatal(err)
				}
			}
			c.clearNurseryMarks()
			c.scanRememberedCards(handleOf(parent))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.clearNurseryMarks()
				c.scanRememberedCards(handleOf(parent))
			}
		})
	}
}

func TestSparseCardBoundsBenchmarkFixtures(t *testing.T) {
	for _, count := range []int{1, 8, 64, 512} {
		c, parent, child := sparseCardBoundsFixture(t, count)
		for i := 0; i < count; i++ {
			if err := c.ArraySet(parent, uint32(i*128), RefValue(child)); err != nil {
				t.Fatal(err)
			}
		}
		if len(c.objectCards) != count {
			t.Fatalf("got %d cards, want %d", len(c.objectCards), count)
		}
		root := Root(parent)
		if err := c.CollectMinor(Slots{&root}); err != nil {
			t.Fatal(err)
		}
		if !c.validObjectRef(child) {
			t.Fatal("minor collection lost child")
		}
	}
}

func BenchmarkGCSparseCardCollectorCreate(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	types := []TypeDesc{leaf}
	cfg := Config{NurseryBytes: 4096, ThroughputHeapBytes: 64 << 10}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := NewCollector(cfg, types)
		if err != nil {
			b.Fatal(err)
		}
		c.Close()
	}
}
