//go:build amd64

package gc

import (
	"fmt"
	"reflect"
	"testing"
)

func BenchmarkFullSparseHandleBlocks(b *testing.B) {
	for _, count := range []int{1024, 65536, 262144} {
		for _, shape := range []string{"one", "cluster", "spread", "dense"} {
			b.Run(fmt.Sprintf("handles%d/%s", count, shape), func(b *testing.B) {
				d, err := NewStructDesc(0, nil)
				if err != nil {
					b.Fatal(err)
				}
				c, err := NewCollector(Config{NurseryBytes: uint32(count * 32), ThroughputHeapBytes: 64 << 20}, []TypeDesc{d})
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				roots := make(RefSliceRoots, 0, count)
				for i := 0; i < count; i++ {
					ref, err := c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					if shape == "dense" || shape == "one" && i == 0 || shape == "cluster" && i < 256 || shape == "spread" && i%256 == 0 {
						roots = append(roots, ref)
					}
				}
				if err = c.CollectFull(roots); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err = c.CollectFull(roots); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				occupancy := reflect.ValueOf(c).Elem().FieldByName("majorBlocks")
				capacity := 0
				if occupancy.IsValid() {
					capacity = occupancy.Cap() * 8
				}
				b.ReportMetric(float64(capacity), "index-cap-B")
				if c.Stats().LiveObjects != uint32(len(roots)) {
					b.Fatal("lost object")
				}
			})
		}
	}
}

func BenchmarkMajorBlockGoAllocationControl(b *testing.B) {
	for _, tc := range []struct {
		profile Profile
		active  bool
	}{{ProfileThroughput, false}, {ProfileTiny, false}, {ProfileThroughput, true}} {
		b.Run(fmt.Sprintf("profile%d/active%v", tc.profile, tc.active), func(b *testing.B) {
			d, err := NewStructDesc(0, nil)
			if err != nil {
				b.Fatal(err)
			}
			c, err := NewCollector(Config{Profile: tc.profile, NurseryBytes: 2 << 20}, []TypeDesc{d})
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			if tc.active {
				for i := 0; i < 65536; i++ {
					if _, err = c.NewStructDefault(0); err != nil {
						b.Fatal(err)
					}
				}
				for i := 0; i < 2; i++ {
					if err = c.CollectFull(EmptyRoots{}); err != nil {
						b.Fatal(err)
					}
				}
			}
			initial := c.Stats().Allocations
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err = c.NewStructDefaultWithRoots(0, EmptyRoots{}); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if c.Stats().Allocations != initial+uint64(b.N) {
				b.Fatal("wrong allocation count")
			}
		})
	}
}
