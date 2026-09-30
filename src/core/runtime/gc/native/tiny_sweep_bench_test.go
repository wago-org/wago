package gc

import (
	"fmt"
	"testing"
)

func BenchmarkTinyStepSweepBlack(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, handles := range []uint32{64, 4096, 65536} {
		b.Run(fmt.Sprintf("handles=%d", handles), func(b *testing.B) {
			c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: handles * 16, TinyBlockBytes: 16}, []TypeDesc{leaf})
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			for i := uint32(0); i < handles; i++ {
				if _, err := c.NewStructDefault(0); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.tinyGC.state = tinySweep
				c.tinyGC.sweep = 1
				c.tinyGC.sweepLimit = uint32(len(c.handles))
				if err := c.Step(nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
