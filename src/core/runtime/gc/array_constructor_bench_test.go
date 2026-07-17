package gc

import (
	"fmt"
	"testing"
)

func BenchmarkArrayConstructors(b *testing.B) {
	for _, n := range []uint32{256, 4096} {
		b.Run(fmt.Sprintf("uniform-i32/%d", n), func(b *testing.B) {
			c := newBulkBenchmarkCollector(b)
			b.ReportAllocs()
			b.SetBytes(int64(n * 4))
			for i := 0; i < b.N; i++ {
				if _, err := c.NewArrayWithRoots(1, n, I32Value(int32(i)), EmptyRoots{}); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("uniform-ref/%d", n), func(b *testing.B) {
			c := newBulkBenchmarkCollector(b)
			child, err := c.NewStructDefault(0)
			if err != nil {
				b.Fatal(err)
			}
			slot, err := c.NewCheckedGlobalSlot(child)
			if err != nil {
				b.Fatal(err)
			}
			root := collectorGlobalRootSlot{collector: c, index: slot}
			b.ReportAllocs()
			b.SetBytes(int64(n * 4))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := c.NewRefArrayWithRoots(2, n, root, Slots{root}); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("fixed-ref/%d", n), func(b *testing.B) {
			c := newBulkBenchmarkCollector(b)
			child, err := c.NewStructDefault(0)
			if err != nil {
				b.Fatal(err)
			}
			slot, err := c.NewCheckedGlobalSlot(child)
			if err != nil {
				b.Fatal(err)
			}
			values := make([]Value, n)
			for i := range values {
				values[i] = RefValue(child)
			}
			root := collectorGlobalRootSlot{collector: c, index: slot}
			b.ReportAllocs()
			b.SetBytes(int64(n * 4))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := c.NewArrayFixedWithRoots(2, values, Slots{root}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type collectorGlobalRootSlot struct {
	collector *Collector
	index     uint32
}

func (s collectorGlobalRootSlot) GetRef() Ref {
	r, _ := s.collector.CheckedGlobalSlot(s.index)
	return r
}

func (s collectorGlobalRootSlot) SetRef(r Ref) {
	_ = s.collector.SetGlobalSlot(s.index, r)
}
