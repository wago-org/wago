package gc

import (
	"strconv"
	"testing"
)

func newBulkBenchmarkCollector(b *testing.B) *Collector {
	obj, _ := NewStructDesc(0, nil)
	i8, _ := NewArrayDesc(1, StorageI8)
	refs, _ := NewArrayDesc(2, StorageRef)
	nullable, _ := NewArrayDesc(3, StorageRefNull)
	i64, _ := NewArrayDesc(4, StorageI64)
	c, err := NewCollector(Config{}, []TypeDesc{obj, i8, refs, nullable, i64})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	return c
}

func BenchmarkArrayBulk(b *testing.B) {
	for _, n := range []uint32{16, 256, 4096} {
		b.Run("numeric-copy-"+benchmarkLength(n), func(b *testing.B) {
			c := newBulkBenchmarkCollector(b)
			src, _ := c.NewArrayDefault(1, n)
			dst, _ := c.NewArrayDefault(1, n)
			b.ReportAllocs()
			b.SetBytes(int64(n))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.ArrayCopy(dst, 0, src, 0, n); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("init-data-"+benchmarkLength(n), func(b *testing.B) {
			c := newBulkBenchmarkCollector(b)
			dst, _ := c.NewArrayDefault(1, n)
			data := make([]byte, n)
			b.ReportAllocs()
			b.SetBytes(int64(n))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.ArrayInitData(dst, 0, data, 0, n); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("init-words-"+benchmarkLength(n), func(b *testing.B) {
			c := newBulkBenchmarkCollector(b)
			dst, _ := c.NewArrayDefault(4, n)
			words := make([]uint64, n)
			b.ReportAllocs()
			b.SetBytes(int64(n * 8))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.ArrayInitWords(dst, 0, words); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("reference-fill-"+benchmarkLength(n), func(b *testing.B) {
			c := newBulkBenchmarkCollector(b)
			dst, _ := c.NewArrayDefault(3, n)
			b.ReportAllocs()
			b.SetBytes(int64(n * 4))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.ArrayFill(dst, 0, Value{Kind: StorageRefNull}, n); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("reference-copy-"+benchmarkLength(n), func(b *testing.B) {
			c := newBulkBenchmarkCollector(b)
			child, _ := c.NewStructDefault(0)
			src, _ := c.NewArray(2, n, RefValue(child))
			dst, _ := c.NewArray(2, n, RefValue(child))
			b.ReportAllocs()
			b.SetBytes(int64(n * 4))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.ArrayCopy(dst, 0, src, 0, n); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("reference-widen-"+benchmarkLength(n), func(b *testing.B) {
			c := newBulkBenchmarkCollector(b)
			child, _ := c.NewStructDefault(0)
			src, _ := c.NewArray(2, n, RefValue(child))
			dst, _ := c.NewArrayDefault(3, n)
			b.ReportAllocs()
			b.SetBytes(int64(n * 4))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.ArrayCopy(dst, 0, src, 0, n); err != nil {
					b.Fatal(err)
				}
			}
		})
		if n > 1 {
			b.Run("reference-overlap-"+benchmarkLength(n), func(b *testing.B) {
				c := newBulkBenchmarkCollector(b)
				child, _ := c.NewStructDefault(0)
				array, _ := c.NewArray(2, n, RefValue(child))
				b.ReportAllocs()
				b.SetBytes(int64((n - 1) * 4))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := c.ArrayCopy(array, 1, array, 0, n-1); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func benchmarkLength(n uint32) string { return strconv.FormatUint(uint64(n), 10) }
