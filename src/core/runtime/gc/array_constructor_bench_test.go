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
			var scratch ArrayInitializerRootScratch
			b.ReportAllocs()
			b.SetBytes(int64(n * 4))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := c.NewArrayWithRootScratch(2, n, RefValue(child), EmptyRoots{}, &scratch); err != nil {
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
			values := make([]Value, n)
			for i := range values {
				values[i] = RefValue(child)
			}
			var scratch ArrayInitializerRootScratch
			b.ReportAllocs()
			b.SetBytes(int64(n * 4))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := c.NewArrayFixedWithRootScratch(2, values, EmptyRoots{}, &scratch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
