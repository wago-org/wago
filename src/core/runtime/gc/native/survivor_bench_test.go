package gc

import (
	"fmt"
	"testing"
)

func newSurvivorBenchCollector(b *testing.B, cfg Config) *Collector {
	b.Helper()
	c, err := NewCollector(cfg, testTypes(b))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	return c
}

func BenchmarkThroughputObjectLifetimes(b *testing.B) {
	for _, lifetime := range []int{0, 1, 2, 3, 5} {
		b.Run(fmt.Sprintf("minors=%d", lifetime), func(b *testing.B) {
			c := newSurvivorBenchCollector(b, Config{
				NurseryBytes: 1 << 20, SurvivorBytes: 512 << 10,
				ThroughputHeapBytes: 16 << 20,
			})
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
				for age := 0; age < lifetime; age++ {
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
			b.ReportMetric(float64(after.YoungBytesCopied-before.YoungBytesCopied)/float64(b.N), "young-copy-B/op")
			b.ReportMetric(float64(after.PromotedBytes-before.PromotedBytes)/float64(b.N), "promoted-B/op")
			b.ReportMetric(float64(after.FullCollections-before.FullCollections)/float64(b.N), "full-GCs/op")
		})
	}
}

func BenchmarkThroughputMixedLifetimeGraph(b *testing.B) {
	for _, moving := range []bool{false, true} {
		name := "immediate-promotion"
		cfg := Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 32 << 20, DisableMovingNursery: true}
		if moving {
			name = "survivor-aging"
			cfg.DisableMovingNursery = false
			cfg.SurvivorBytes = 512 << 10
		}
		b.Run(name, func(b *testing.B) {
			c := newSurvivorBenchCollector(b, cfg)
			var medium Root
			roots := Slots{&medium}
			before := c.Stats()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// One object dies without surviving; one survives exactly one minor.
				if _, err := c.NewStructDefault(0); err != nil {
					b.Fatal(err)
				}
				ref, err := c.NewStructDefault(0)
				if err != nil {
					b.Fatal(err)
				}
				medium = Root(ref)
				if err := c.CollectMinor(roots); err != nil {
					b.Fatal(err)
				}
				medium = Root(Null())
				if err := c.CollectMinor(roots); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			after := c.Stats()
			b.ReportMetric(float64(after.YoungBytesCopied-before.YoungBytesCopied)/float64(b.N), "young-copy-B/op")
			b.ReportMetric(float64(after.PromotedBytes-before.PromotedBytes)/float64(b.N), "promoted-B/op")
			b.ReportMetric(float64(after.FullCollections-before.FullCollections)/float64(b.N), "full-GCs/op")
		})
	}
}
