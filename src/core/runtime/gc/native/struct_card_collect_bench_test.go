package gc

import (
	"fmt"
	"testing"
)

func BenchmarkGCStructCardScanControls(b *testing.B) {
	for _, fields := range []int{255, 256, 257, 4097} {
		for _, count := range []int{15, 16, 32, 33} {
			if fields < 4097 && count > 16 {
				continue
			}
			b.Run(fmt.Sprintf("fields=%d/ranges=%d", fields, count), func(b *testing.B) {
				f := newStructRangeFixture(b, fields, count, "shuffled", true, false, fields < 4097, 11)
				c := f.c
				h := handleOf(f.parent)
				c.clearNurseryMarks()
				c.scanRememberedCards(h)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					c.clearNurseryMarks()
					c.scanRememberedCards(h)
				}
			})
		}
	}
	for _, count := range []int{1, 15, 16, 32, 33} {
		b.Run(fmt.Sprintf("array/ranges=%d", count), func(b *testing.B) {
			f := newStructRangeFixture(b, 4097, 16, "ordered", true, false, false, 1)
			c := f.c
			desc, err := NewArrayDesc(2, StorageRefNull)
			if err != nil {
				b.Fatal(err)
			}
			if err = c.AddTypes([]TypeDesc{desc}); err != nil {
				b.Fatal(err)
			}
			parent, err := c.NewArrayDefault(2, 8193)
			if err != nil {
				b.Fatal(err)
			}
			if err = c.ForcePromote(parent); err != nil {
				b.Fatal(err)
			}
			child := mustStructCardChild(b, f)
			for i := 0; i < count; i++ {
				if err = c.ArraySet(parent, uint32(i*64), RefValue(child)); err != nil {
					b.Fatal(err)
				}
			}
			if structRangeCount(b, c, parent) != count {
				b.Fatal("array range count")
			}
			h := handleOf(parent)
			c.clearNurseryMarks()
			c.scanRememberedCards(h)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.clearNurseryMarks()
				c.scanRememberedCards(h)
			}
		})
	}
}

// Each timed operation collects a fresh young generation. Construction, checks,
// and full-collection cleanup are outside collection-only timing.
func BenchmarkGCStructCardCollectMinor(b *testing.B) {
	for _, moving := range []bool{false, true} {
		for _, count := range []int{15, 16, 32, 33} {
			for _, mixed := range []bool{false, true} {
				b.Run(fmt.Sprintf("moving=%t/ranges=%d/mixed=%t", moving, count, mixed), func(b *testing.B) {
					f := newStructRangeFixture(b, 4097, count, "shuffled", moving, false, false, 13)
					c := f.c
					root := Root(f.parent)
					roots := Slots{&root}
					var array Ref
					var arrayRoot Root
					if mixed {
						desc, err := NewArrayDesc(2, StorageRefNull)
						if err != nil {
							b.Fatal(err)
						}
						if err = c.AddTypes([]TypeDesc{desc}); err != nil {
							b.Fatal(err)
						}
						array, err = c.NewArrayDefault(2, 4097)
						if err != nil {
							b.Fatal(err)
						}
						if err = c.ForcePromote(array); err != nil {
							b.Fatal(err)
						}
						arrayRoot = Root(array)
						roots = append(roots, &arrayRoot)
					}
					b.ReportAllocs()
					b.ResetTimer()
					b.StopTimer()
					for i := 0; i < b.N; i++ {
						if i > 0 {
							f.populate(b)
						}
						if mixed {
							for n := 0; n < 16; n++ {
								child, err := c.NewStructDefault(0)
								if err != nil {
									b.Fatal(err)
								}
								if err = c.StructSet(child, 0, I32Value(int32(n+701))); err != nil {
									b.Fatal(err)
								}
								if err = c.ArraySet(array, uint32(n*64), RefValue(child)); err != nil {
									b.Fatal(err)
								}
							}
						}
						if structRangeCount(b, c, f.parent) != count {
							b.Fatal("collection range count")
						}
						if mixed && structRangeCount(b, c, array) != 16 {
							b.Fatal("mixed range count")
						}
						b.StartTimer()
						err := c.CollectMinor(roots)
						b.StopTimer()
						if err != nil {
							b.Fatal(err)
						}
						f.parent = Ref(root)
						f.check(b, moving)
						if mixed {
							for n := 0; n < 16; n++ {
								v, err := c.ArrayGet(array, uint32(n*64))
								if err != nil {
									b.Fatal(err)
								}
								payload, err := c.StructGet(v.Ref, 0)
								if err != nil || int32(payload.Bits) != int32(n+701) {
									b.Fatal("mixed child lost")
								}
								if err = c.ArraySet(array, uint32(n*64), RefValue(Null())); err != nil {
									b.Fatal(err)
								}
							}
						}
						for _, field := range f.fields {
							if err = c.StructSet(f.parent, field, RefValue(Null())); err != nil {
								b.Fatal(err)
							}
						}
						if err = c.CollectFull(roots); err != nil {
							b.Fatal(err)
						}
						if err = c.Verify(roots); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func BenchmarkGCStructCardScanSparseReferences(b *testing.B) {
	for _, count := range []int{15, 16, 32, 33} {
		b.Run(fmt.Sprintf("ranges=%d", count), func(b *testing.B) {
			f := newStructRangeFixture(b, 4097, count, "shuffled-sparse", true, false, false, 11)
			c := f.c
			h := handleOf(f.parent)
			c.clearNurseryMarks()
			c.scanRememberedCards(h)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.clearNurseryMarks()
				c.scanRememberedCards(h)
			}
		})
	}
}
