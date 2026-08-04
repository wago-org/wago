package gc

import "testing"

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
	for i := 0; i < b.N; i++ {
		cycle()
	}
}
