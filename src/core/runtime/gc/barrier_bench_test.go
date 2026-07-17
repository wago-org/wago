package gc

import "testing"

func BenchmarkRememberedArrayWrite(b *testing.B) {
	for _, profile := range []struct {
		name string
		cfg  Config
	}{
		{name: "throughput", cfg: Config{StressNurseryBytes: 1 << 20}},
		{name: "tiny", cfg: Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 8}},
	} {
		b.Run(profile.name, func(b *testing.B) {
			obj, _ := NewStructDesc(0, nil)
			i32, _ := NewArrayDesc(1, StorageI32)
			i64, _ := NewArrayDesc(2, StorageI64)
			refs, _ := NewArrayDesc(3, StorageRefNull)
			c, err := NewCollector(profile.cfg, []TypeDesc{obj, i32, i64, refs})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(c.Close)
			array, err := c.NewArrayDefault(3, 4096)
			if err != nil {
				b.Fatal(err)
			}
			if err := c.ForcePromote(array); err != nil {
				b.Fatal(err)
			}
			child, err := c.NewStructDefault(0)
			if err != nil {
				b.Fatal(err)
			}
			value := RefValue(child)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.ArraySet(array, uint32(i)&4095, value); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(c.RememberedCount()), "remembered")
			b.ReportMetric(float64(c.CardCount()), "cards")
		})
	}
}
