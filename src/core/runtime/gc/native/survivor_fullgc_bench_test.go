package gc

import (
	"errors"
	"testing"
)

func BenchmarkThroughputMixedLifetimeFullGCPressure(b *testing.B) {
	for _, moving := range []bool{false, true} {
		name := "immediate-promotion"
		cfg := Config{
			NurseryBytes: 1024, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096,
			DisableMovingNursery: true,
		}
		if moving {
			name = "survivor-aging"
			cfg.DisableMovingNursery = false
			cfg.SurvivorBytes = 512
		}
		b.Run(name, func(b *testing.B) {
			c := newSurvivorBenchCollector(b, cfg)
			var root Root
			roots := Slots{&root}
			before := c.Stats()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ref, err := c.NewStructDefault(0)
				if err != nil {
					b.Fatal(err)
				}
				root = Root(ref)
				if err := c.CollectMinor(roots); err != nil {
					if !errors.Is(err, errThroughputHeapExhausted) {
						b.Fatal(err)
					}
					if err := c.CollectFull(roots); err != nil {
						b.Fatal(err)
					}
					if err := c.CollectMinor(roots); err != nil {
						b.Fatal(err)
					}
				}
				root = Root(Null())
				if err := c.CollectMinor(roots); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			after := c.Stats()
			b.ReportMetric(float64(after.FullCollections-before.FullCollections)/float64(b.N), "full-GCs/op")
			b.ReportMetric(float64(after.PromotedBytes-before.PromotedBytes)/float64(b.N), "promoted-B/op")
		})
	}
}
