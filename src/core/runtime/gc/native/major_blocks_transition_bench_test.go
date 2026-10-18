//go:build amd64

package gc

import (
	"fmt"
	"testing"
)

func BenchmarkFullHandleBlockTransitions(b *testing.B) {
	for _, count := range []int{65536, 262144} {
		for _, phase := range []string{"dense-to-sparse", "second-sparse", "sparse-to-dense"} {
			b.Run(fmt.Sprintf("handles%d/%s", count, phase), func(b *testing.B) {
				d, err := NewStructDesc(0, nil)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					c, err := NewCollector(Config{NurseryBytes: uint32(count * 32), ThroughputHeapBytes: 64 << 20}, []TypeDesc{d})
					if err != nil {
						b.Fatal(err)
					}
					roots := make(RefSliceRoots, count)
					for j := range roots {
						roots[j], err = c.NewStructDefault(0)
						if err != nil {
							b.Fatal(err)
						}
					}
					selected := roots[:1]
					if phase == "dense-to-sparse" {
						selected = roots
					}
					if err = c.CollectFull(selected); err != nil {
						b.Fatal(err)
					}
					selected = roots[:1]
					if phase == "sparse-to-dense" {
						if err = c.CollectFull(selected); err != nil {
							b.Fatal(err)
						}
						for j := 1; j < count; j++ {
							roots[j], err = c.NewStructDefault(0)
							if err != nil {
								b.Fatal(err)
							}
						}
						selected = roots
					}
					b.StartTimer()
					if err = c.CollectFull(selected); err != nil {
						b.Fatal(err)
					}
					b.StopTimer()
					if got := c.Stats().LiveObjects; got != uint32(len(selected)) {
						b.Fatalf("live=%d want=%d", got, len(selected))
					}
					c.Close()
					b.StartTimer()
				}
			})
		}
	}
}
