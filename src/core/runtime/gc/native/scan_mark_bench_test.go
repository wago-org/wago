//go:build amd64 || arm64

package gc

import (
	"fmt"
	"testing"
)

func BenchmarkNativeMarkArrayTraversal(b *testing.B) {
	for _, count := range []int{16, 4096} {
		for _, mode := range []string{"full", "nursery"} {
			b.Run(fmt.Sprintf("refs%d/%s", count, mode), func(b *testing.B) {
				leaf, err := NewStructDesc(0, nil)
				if err != nil {
					b.Fatal(err)
				}
				array, err := NewArrayDesc(1, StorageRefNull)
				if err != nil {
					b.Fatal(err)
				}
				c, err := NewCollector(Config{NurseryBytes: 1 << 20}, []TypeDesc{leaf, array})
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				parent, err := c.NewArrayDefault(1, uint32(count))
				if err != nil {
					b.Fatal(err)
				}
				children := make([]Ref, count)
				for i := range children {
					child, err := c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					children[i] = child
					if err := c.ArraySet(parent, uint32(i), RefValue(child)); err != nil {
						b.Fatal(err)
					}
				}
				run := func() {
					c.clearMarks()
					if mode == "full" {
						c.markRef(parent)
						c.drainMarkStack()
					} else {
						c.markNurseryRef(parent)
						c.drainNurseryMarkStack()
					}
				}
				run() // warm the reusable mark stack outside the timed region
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					run()
				}
				b.StopTimer()
				for _, child := range children {
					if !c.mark[handleOf(child)] {
						b.Fatal("missed child")
					}
				}
			})
		}
	}
}
