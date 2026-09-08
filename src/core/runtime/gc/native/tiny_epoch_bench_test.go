package gc

import (
	"fmt"
	"testing"
)

func BenchmarkTinyCycleStartHandleReset(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, handles := range []uint32{256, 4096, 65536} {
		b.Run(fmt.Sprintf("handles=%d", handles), func(b *testing.B) {
			heapBytes := handles * 16
			if heapBytes < 4096 {
				heapBytes = 4096
			}
			c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: heapBytes, TinyBlockBytes: 16}, []TypeDesc{leaf})
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
			b.ReportMetric(float64(handles), "handles")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Isolate cycle initialization. Abandoning the rootless mark phase is
				// intentional here: completing sweep would destroy the fixed live
				// handle population whose reset cost this benchmark controls.
				c.tinyGC.state = tinyIdle
				if err := c.Step(nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
