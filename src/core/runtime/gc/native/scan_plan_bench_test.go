//go:build amd64

package gc

import (
	"fmt"
	"testing"
)

func BenchmarkFullStructScanPlan(b *testing.B) {
	for _, fields := range []int{8, 12, 64, 256} {
		for _, shape := range []string{"sparse", "quarter", "dense", "scalar"} {
			b.Run(fmt.Sprintf("fields%d/%s", fields, shape), func(b *testing.B) {
				kinds := make([]StorageKind, fields)
				for i := range kinds {
					kinds[i] = StorageI64
					if shape == "dense" || shape == "sparse" && i%16 == 15 || shape == "quarter" && i%4 == 3 {
						kinds[i] = StorageRefNull
					}
				}
				if shape == "sparse" {
					kinds[fields-1] = StorageRefNull
				}
				d, err := NewStructDesc(0, kinds)
				if err != nil {
					b.Fatal(err)
				}
				c, err := NewCollector(Config{NurseryBytes: 16 << 20, ThroughputHeapBytes: 64 << 20}, []TypeDesc{d})
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				roots := make(RefSliceRoots, 2048)
				for i := range roots {
					ref, err := c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					roots[i] = ref
					if i != 0 {
						for j, k := range kinds {
							if k == StorageRefNull {
								if err := c.StructSet(ref, uint32(j), RefValue(roots[i-1])); err != nil {
									b.Fatal(err)
								}
							}
						}
					}
				}
				if err := c.CollectFull(roots); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := c.CollectFull(roots); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				if c.Stats().LiveObjects != uint32(len(roots)) {
					b.Fatal("lost live object")
				}
			})
		}
	}
}

// Measure construction separately from collection, including scan-plan work.
func BenchmarkStructScanPlanSetup(b *testing.B) {
	for _, fields := range []int{8, 64, 256} {
		b.Run(fmt.Sprintf("fields%d", fields), func(b *testing.B) {
			kinds := make([]StorageKind, fields)
			for i := range kinds {
				kinds[i] = StorageI64
			}
			kinds[fields-1] = StorageRefNull
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				d, err := NewStructDesc(0, kinds)
				if err != nil || len(d.Fields) != fields {
					b.Fatal("invalid descriptor", err)
				}
			}
		})
	}
}
