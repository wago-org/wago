package gc

import (
	"testing"
	"unsafe"
)

func BenchmarkMinorPromotionScratch(b *testing.B) {
	obj, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{
		StressNurseryBytes:  4096,
		ThroughputHeapBytes: 4096,
		ThroughputPageBytes: 4096,
	}, []TypeDesc{obj})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)

	var root Root
	roots := Slots{&root}
	cycle := func() {
		r, err := c.NewStructDefault(0)
		if err != nil {
			b.Fatal(err)
		}
		root = Root(r)
		if err := c.CollectMinor(roots); err != nil {
			b.Fatal(err)
		}
		root = Root(Null())
		if err := c.CollectFull(nil); err != nil {
			b.Fatal(err)
		}
	}
	cycle() // populate reusable handle, allocator, mark, and promotion storage
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(unsafe.Sizeof(plannedPromotion{})), "plan-B")
	for i := 0; i < b.N; i++ {
		cycle()
	}
}

func BenchmarkForcePromoteTransactional(b *testing.B) {
	obj, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{
		NurseryBytes:        4096,
		ThroughputHeapBytes: 1 << 20,
		ThroughputPageBytes: 4096,
	}, []TypeDesc{obj})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)

	cycle := func() {
		r, err := c.NewStructDefault(0)
		if err != nil {
			b.Fatal(err)
		}
		if err := c.ForcePromote(r); err != nil {
			b.Fatal(err)
		}
	}
	cycle()
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i != 0 && i%128 == 0 {
			b.StopTimer()
			if err := c.CollectFull(EmptyRoots{}); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
		}
		cycle()
	}
}
