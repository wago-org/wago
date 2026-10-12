package gc

import "testing"

func BenchmarkMinorPromotionMixedSizes(b *testing.B) {
	for _, moving := range []bool{false, true} {
		name := "immediate"
		if moving {
			name = "survivor-aging"
		}
		b.Run(name, func(b *testing.B) {
			var types []TypeDesc
			for i, fields := range []int{1, 2, 3, 4, 5, 8, 12, 20, 28, 44, 60} {
				storage := make([]StorageKind, fields)
				for j := range storage {
					storage[j] = StorageI32
				}
				desc, err := NewStructDesc(TypeID(i), storage)
				if err != nil {
					b.Fatal(err)
				}
				types = append(types, desc)
			}
			c, err := NewCollector(Config{
				NurseryBytes:         1 << 20,
				ThroughputHeapBytes:  4 << 20,
				ThroughputPageBytes:  4096,
				DisableMovingNursery: !moving,
			}, types)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(c.Close)
			live := make([]Root, 1024)
			roots := make(Slots, len(live))
			for i := range roots {
				roots[i] = &live[i]
			}
			cycle := func() {
				if moving {
					// Keep this two-minor workload independent of adaptation after
					// the full collection used to reset the previous cycle.
					c.tenuringThreshold = defaultTenuringThreshold
				}
				for i := range live {
					typeID := TypeID((i*17 + i/7) % len(types))
					ref, err := c.NewStructDefault(typeID)
					if err != nil {
						b.Fatal(err)
					}
					live[i] = Root(ref)
				}
				if err := c.CollectMinor(roots); err != nil {
					b.Fatal(err)
				}
				if moving {
					c.tenuringThreshold = defaultTenuringThreshold
					if err := c.CollectMinor(roots); err != nil {
						b.Fatal(err)
					}
				}
				if c.entry(Ref(live[0])).space != spaceOld {
					b.Fatal("mixed-size cycle did not promote its retained objects")
				}
				clear(live)
				if err := c.CollectFull(nil); err != nil {
					b.Fatal(err)
				}
			}
			cycle()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cycle()
			}
		})
	}
}
