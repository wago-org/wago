package gc_test

import (
	"fmt"
	"github.com/wago-org/wago/src/core/runtime/gc"
	"testing"
)

func BenchmarkCheckedCollectionRoots(b *testing.B) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		for _, count := range []int{0, 1, 16, 256, 4096} {
			b.Run(fmt.Sprintf("profile=%d/roots=%d", profile, count), func(b *testing.B) {
				c := collector(b, profile)
				roots := make(gc.RefSliceRoots, count)
				for i := range roots {
					roots[i] = gc.I31New(int32(i))
				}
				var set gc.RootSet = roots
				if count == 0 {
					set = nil
				}
				if err := c.CollectFull(set); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := c.CollectFull(set); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkCheckedConstructors(b *testing.B) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		b.Run(fmt.Sprint(profile), func(b *testing.B) {
			c := collector(b, profile)
			values := []gc.Value{gc.I32Value(1), gc.I32Value(2), gc.I32Value(3), gc.I32Value(4)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := c.NewArrayFixedWithRoots(1, values, gc.EmptyRoots{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
