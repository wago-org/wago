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
		b.Run("reference-fill-no-barrier-"+benchmarkLength(n), func(b *testing.B) {
			c := newBulkBenchmarkCollector(b)
			dst, _ := c.NewArrayDefault(3, n)
			b.ReportAllocs()
			b.SetBytes(int64(n * 4))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.ArrayFillNoBarrier(dst, 0, Value{Kind: StorageRefNull}, n); err != nil {
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

// BenchmarkArrayDeferredReferenceBatch quantifies the validation removed from
// preflighted array.init_elem writes. The hardening variant deliberately repeats
// ownership checks and is the differential oracle for the release path.
func BenchmarkArrayDeferredReferenceBatch(b *testing.B) {
	for _, n := range []uint32{16, 256, 4096} {
		for _, hardening := range []bool{false, true} {
			name := "prevalidated-"
			if hardening {
				name = "revalidate-"
			}
			b.Run(name+benchmarkLength(n), func(b *testing.B) {
				obj, _ := NewStructDesc(0, nil)
				refs, _ := NewArrayDesc(1, StorageRefNull)
				c, err := NewCollector(Config{StressNurseryBytes: 1 << 20, VerifyAfterCollect: hardening}, []TypeDesc{obj, refs})
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(c.Close)
				dst, err := c.NewArrayDefault(1, n)
				if err != nil {
					b.Fatal(err)
				}
				if err := c.ForcePromote(dst); err != nil {
					b.Fatal(err)
				}
				child, err := c.NewStructDefault(0)
				if err != nil {
					b.Fatal(err)
				}
				value := RefValue(child)
				b.ReportAllocs()
				b.SetBytes(int64(n * 4))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for index := uint32(0); index < n; index++ {
						if err := c.ArraySetDeferredBarrier(dst, index, value); err != nil {
							b.Fatal(err)
						}
					}
					c.PostBulkWriteBarrier(dst, 0, n)
				}
			})
		}
	}
}

func benchmarkLength(n uint32) string { return strconv.FormatUint(uint64(n), 10) }
