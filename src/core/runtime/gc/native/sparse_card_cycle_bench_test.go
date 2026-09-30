package gc

import (
	"fmt"
	"math/rand"
	"testing"
)

// Interleaving parents defeats the single-parent proof. Keep this negative
// control beside the ascending single-parent best case.
func BenchmarkGCSparseCardInsertMixed(b *testing.B) {
	for _, count := range []int{8, 64, 512} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			c, parent, child := sparseCardBoundsFixture(b, count)
			other, err := c.NewArrayDefault(1, uint32(count*128+1))
			if err != nil {
				b.Fatal(err)
			}
			if err = c.ForcePromote(other); err != nil {
				b.Fatal(err)
			}
			run := func() {
				c.clearCardMetadata()
				for i := 0; i < count; i++ {
					for _, p := range []Ref{parent, other} {
						if err := c.ArraySet(p, uint32(i*128), RefValue(child)); err != nil {
							b.Fatal(err)
						}
					}
				}
			}
			run()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				run()
			}
		})
	}
}

// Time a complete steady-state lifecycle, including writes, unique live young
// objects, minor evacuation, clearing edges and full reclamation. The sparse
// layout is intentionally synthetic; this is not an application benchmark.
func BenchmarkGCSparseCardFullCycle(b *testing.B) {
	for _, moving := range []bool{false, true} {
		for _, count := range []int{8, 64, 512} {
			for _, order := range []string{"ascending", "random"} {
				b.Run(fmt.Sprintf("moving=%t/cards=%d/%s", moving, count, order), func(b *testing.B) {
					c, parent, _ := sparseCardBoundsFixtureConfig(b, count, Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 4 << 20, DisableMovingNursery: !moving})
					root := Root(parent)
					roots := Slots{&root}
					indexes := make([]int, count)
					for i := range indexes {
						indexes[i] = i
					}
					if order == "random" {
						rand.New(rand.NewSource(641)).Shuffle(count, func(i, j int) { indexes[i], indexes[j] = indexes[j], indexes[i] })
					}
					run := func() {
						for _, index := range indexes {
							child, err := c.NewStructDefault(0)
							if err != nil {
								b.Fatal(err)
							}
							if err = c.ArraySet(parent, uint32(index*128), RefValue(child)); err != nil {
								b.Fatal(err)
							}
						}
						if err := c.CollectMinor(roots); err != nil {
							b.Fatal(err)
						}
						for i := 0; i < count; i++ {
							if err := c.ArraySet(parent, uint32(i*128), RefValue(Null())); err != nil {
								b.Fatal(err)
							}
						}
						if err := c.CollectFull(roots); err != nil {
							b.Fatal(err)
						}
					}
					run()
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						run()
					}
				})
			}
		}
	}
}
