package gc

import "testing"

func newBulkBenchmarkCollector(b *testing.B) *Collector {
	obj, _ := NewStructDesc(0, nil)
	i8, _ := NewArrayDesc(1, StorageI8)
	refs, _ := NewArrayDesc(2, StorageRef)
	nullable, _ := NewArrayDesc(3, StorageRefNull)
	c, err := NewCollector(Config{}, []TypeDesc{obj, i8, refs, nullable})
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
	}
}

func benchmarkLength(n uint32) string {
	switch n {
	case 16:
		return "16"
	case 256:
		return "256"
	default:
		return "4096"
	}
}
