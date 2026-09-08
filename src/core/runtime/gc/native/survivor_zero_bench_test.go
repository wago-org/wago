package gc

import "testing"

func BenchmarkThroughputZeroSurvivalPolicy(b *testing.B) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{name: "immediate-promotion", cfg: Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 16 << 20, DisableMovingNursery: true}},
		{name: "survivor-aging", cfg: Config{NurseryBytes: 1 << 20, SurvivorBytes: 512 << 10, ThroughputHeapBytes: 16 << 20}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			c := newSurvivorBenchCollector(b, tc.cfg)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := c.NewStructDefault(0); err != nil {
					b.Fatal(err)
				}
				if err := c.CollectMinor(nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
