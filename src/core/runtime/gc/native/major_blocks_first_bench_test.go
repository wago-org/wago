//go:build amd64

package gc

import (
	"fmt"
	"testing"
)

func BenchmarkFullFirstHandleBlocks(b *testing.B) {
	for _, count := range []int{4096, 65536} {
		for _, dense := range []bool{false, true} {
			b.Run(fmt.Sprintf("handles%d/dense%v", count, dense), func(b *testing.B) {
				d, err := NewStructDesc(0, nil)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					c, err := NewCollector(Config{NurseryBytes: uint32(count * 32), ThroughputHeapBytes: 8 << 20}, []TypeDesc{d})
					if err != nil {
						b.Fatal(err)
					}
					roots := make(RefSliceRoots, 0, count)
					for j := 0; j < count; j++ {
						ref, err := c.NewStructDefault(0)
						if err != nil {
							b.Fatal(err)
						}
						if dense || j == 0 {
							roots = append(roots, ref)
						}
					}
					b.StartTimer()
					if err = c.CollectFull(roots); err != nil {
						b.Fatal(err)
					}
					b.StopTimer()
					if c.Stats().LiveObjects != uint32(len(roots)) {
						b.Fatal("lost object")
					}
					c.Close()
					b.StartTimer()
				}
			})
		}
	}
}
