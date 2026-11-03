package gc

import (
	"fmt"
	"testing"
)

// A single root exposes a shared, cyclic graph through a reference array.
// Setup is outside the timer; repeated full collections measure collector wall time.
func BenchmarkThroughputParallelGraph(b *testing.B) {
	for _, workers := range []int{1, 4} {
		for _, edges := range []int{2, 32} {
			b.Run(fmt.Sprintf("workers=%d/edges=%d", workers, edges), func(b *testing.B) {
				fields := make([]StorageKind, edges)
				for i := range fields {
					fields[i] = StorageRefNull
				}
				node, err := NewStructDesc(0, fields)
				if err != nil {
					b.Fatal(err)
				}
				array, err := NewArrayDesc(1, StorageRefNull)
				if err != nil {
					b.Fatal(err)
				}
				cfg := Config{Workers: workers, NurseryBytes: 32 << 20, ThroughputHeapBytes: 64 << 20}
				c, err := NewCollector(cfg, []TypeDesc{node, array})
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(c.Close)
				const count = 32768
				refs := make([]Ref, count)
				parent, err := c.NewArrayDefault(1, count)
				if err != nil {
					b.Fatal(err)
				}
				for i := range refs {
					refs[i], err = c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					if err = c.ArraySet(parent, uint32(i), RefValue(refs[i])); err != nil {
						b.Fatal(err)
					}
				}
				for i, ref := range refs {
					for field := range fields {
						if err = c.StructSet(ref, uint32(field), RefValue(refs[(i+field+1)%count])); err != nil {
							b.Fatal(err)
						}
					}
				}
				root := Root(parent)
				roots := Slots{&root}
				if err = c.CollectFull(roots); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if err = c.CollectFull(roots); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// Setup/refill and full reclamation are excluded: measure the complete minor
// pause, including root admission, tracing, transactional planning and copying.
func BenchmarkThroughputParallelMinor(b *testing.B) {
	for _, workers := range []int{1, 4} {
		for _, immediate := range []bool{false, true} {
			b.Run(fmt.Sprintf("workers=%d/immediate=%v", workers, immediate), func(b *testing.B) {
				fields := make([]StorageKind, 32)
				for i := range fields {
					fields[i] = StorageI64
				}
				fields[0] = StorageRefNull
				node, err := NewStructDesc(0, fields)
				if err != nil {
					b.Fatal(err)
				}
				array, err := NewArrayDesc(1, StorageRefNull)
				if err != nil {
					b.Fatal(err)
				}
				cfg := Config{Workers: workers, NurseryBytes: 16 << 20, ThroughputHeapBytes: 32 << 20, DisableMovingNursery: immediate}
				c, err := NewCollector(cfg, []TypeDesc{node, array})
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				const count = 16384
				roots := RefSliceRoots{Null()}
				fill := func() {
					parent, err := c.NewArrayDefault(1, count)
					if err != nil {
						b.Fatal(err)
					}
					previous := Null()
					for i := range count {
						r, err := c.NewStructDefault(0)
						if err != nil {
							b.Fatal(err)
						}
						if err = c.StructSet(r, 0, RefValue(previous)); err != nil {
							b.Fatal(err)
						}
						if err = c.ArraySet(parent, uint32(i), RefValue(r)); err != nil {
							b.Fatal(err)
						}
						previous = r
					}
					roots[0] = parent
				}
				fill()
				if err = c.CollectMinor(roots); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					b.StopTimer()
					if err = c.CollectFull(nil); err != nil {
						b.Fatal(err)
					}
					fill()
					b.StartTimer()
					if err = c.CollectMinor(roots); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
