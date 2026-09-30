package gc

import (
	"fmt"
	"testing"
)

// Cold measures allocation of only the reusable root buffer; object tracing
// storage is warmed in both cases. Roots are boxed once outside the timed loop.
func BenchmarkTinyTransientRootStaging(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, cold := range []bool{false, true} {
		for _, count := range []int{0, 1, 256, 4096} {
			b.Run(fmt.Sprintf("cold=%v/roots=%d", cold, count), func(b *testing.B) {
				c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				refs := make(RefSliceRoots, count)
				if count != 0 {
					object, err := c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					for i := range refs {
						refs[i] = object
					}
				}
				var roots RootSet = refs
				if err := c.CollectFull(roots); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if cold {
						c.markStack = nil
					}
					if err := c.CollectFull(roots); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(cap(c.markStack)*4), "root-buffer-bytes")
			})
		}
	}
}

func BenchmarkTinyFullWithDirectRoot(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	object, err := c.NewStructDefault(0)
	if err != nil {
		b.Fatal(err)
	}
	root := Root(object)
	roots := Slots{&root}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.CollectFull(roots); err != nil {
			b.Fatal(err)
		}
	}
}
