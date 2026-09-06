package gc

import "testing"

func BenchmarkTinyAllocationDebtStep(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.tinyGC.state = tinyIdle
		c.tinyGC.rootPhase = tinyRootsNone
		c.tinyGC.sweep = 1
		c.tinyGC.allocationDebt = tinyAllocationDebtBytes
		if err := c.tinyPayAllocationDebt(EmptyRoots{}); err != nil {
			b.Fatal(err)
		}
	}
}
