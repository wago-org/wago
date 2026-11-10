//go:build amd64 || arm64

package gc

import (
	"fmt"
	"reflect"
	"testing"
)

func majorBlocksBenchSparse(b *testing.B) *Collector {
	return majorBlocksBenchSparseChecked(b, false)
}
func majorBlocksBenchSparseChecked(b *testing.B, checked bool) *Collector {
	b.Helper()
	desc, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 2 << 20, ThroughputHeapBytes: 8 << 20}, []TypeDesc{desc})
	if err != nil {
		b.Fatal(err)
	}
	if checked {
		if err := c.EnableCheckedHandles(); err != nil {
			b.Fatal(err)
		}
	}
	b.Cleanup(c.Close)
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
	return c
}

func majorBlocksBenchReport(b *testing.B, c *Collector) {
	occupancy := reflect.ValueOf(c).Elem().FieldByName("majorBlocks")
	capacity := 0
	if occupancy.IsValid() {
		capacity = occupancy.Cap() * 8
		if occupancy.Len() == 0 {
			b.Fatal("candidate sparse index unexpectedly disabled")
		}
	}
	b.ReportMetric(float64(capacity), "index-cap-B")
}

func BenchmarkMajorBlocksActiveNativeReservation(b *testing.B) {
	for _, checked := range []bool{false, true} {
		for _, spread := range []bool{false, true} {
			b.Run(fmt.Sprintf("checked%v/spread%v", checked, spread), func(b *testing.B) {
				c := majorBlocksBenchSparseChecked(b, checked)
				if spread {
					for i := range c.freeHandles {
						c.freeHandles[i] = uint32((i*257)%65536 + 1)
					}
				}
				contiguous := 0
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if !c.PrepareNativeStructHandles() {
						b.Fatal("reservation failed")
					}
					if c.nativeStructAlloc.HandleBase != 0 {
						contiguous++
					}
					c.CancelNativeAllocationBatch()
				}
				b.StopTimer()
				majorBlocksBenchReport(b, c)
				b.ReportMetric(float64(contiguous)/float64(b.N), "contiguous/op")
			})
		}
	}
}

func BenchmarkMajorBlocksAllocateFullCycle(b *testing.B) {
	for _, batch := range []int{1, 64, 512} {
		b.Run(fmt.Sprint(batch), func(b *testing.B) {
			c := majorBlocksBenchSparse(b)
			roots := RefSliceRoots{Null()}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := 0; j < batch; j++ {
					ref, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
					if err != nil {
						b.Fatal(err)
					}
					roots[0] = ref
				}
				if err := c.CollectFull(roots); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			majorBlocksBenchReport(b, c)
			if c.Stats().LiveObjects != 1 {
				b.Fatal("cycle lost object")
			}
		})
	}
}

// This measures the native reservation/publication protocol and full collection;
// object metadata is written in Go, so it is not a Wasm instruction benchmark.
func BenchmarkMajorBlocksNativePublishFullCycle(b *testing.B) {
	for _, batch := range []int{1, 512} {
		b.Run(fmt.Sprint(batch), func(b *testing.B) {
			c := majorBlocksBenchSparse(b)
			roots := RefSliceRoots{Null()}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if c.nurseryBump > c.edenBytes()-uint32(batch)*16 {
					if err := c.CollectMinor(EmptyRoots{}); err != nil {
						b.Fatal(err)
					}
				}
				if !c.PrepareNativeStructHandles() {
					b.Fatal("native reservation failed")
				}

				for j := 0; j < batch; j++ {
					if c.nativeStructAlloc.Cursor >= c.nativeStructAlloc.Count {
						if !c.PrepareNativeStructHandles() {
							b.Fatal("native refill failed")
						}
					}
					cursor := c.nativeStructAlloc.Cursor
					h := c.nativeStructAlloc.Handles[cursor]
					off := c.nurseryBump
					c.handles[h] = handleEntry{off: off, size: 16, space: spaceNursery}
					roots[0] = makeObjRef(h)
					c.writeHeader(roots[0], ObjHeader{TypeID: 0, Size: 16, Flags: FlagPointerFree})
					c.nurseryBump = off + 16
					c.nativeStructAlloc.Cursor = cursor + 1
				}
				if err := c.CollectFull(roots); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			majorBlocksBenchReport(b, c)
			if c.Stats().LiveObjects != 1 {
				b.Fatal("native cycle leaked or lost objects")
			}
		})
	}
}

func BenchmarkMajorBlocksNativeDenseTransition(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		c := majorBlocksBenchSparse(b)
		roots := make(RefSliceRoots, 0, 65536)
		for len(roots) < cap(roots) {
			if !c.PrepareNativeStructHandles() {
				b.Fatal("native refill failed")
			}
			off := c.nurseryBump
			batch := int(c.nativeStructAlloc.Count)
			for j := 0; j < batch; j++ {
				h := c.nativeStructAlloc.Handles[j]
				c.handles[h] = handleEntry{off: off + uint32(j)*16, size: 16, space: spaceNursery}
				ref := makeObjRef(h)
				roots = append(roots, ref)
				c.writeHeader(ref, ObjHeader{TypeID: 0, Size: 16, Flags: FlagPointerFree})
			}
			c.nurseryBump = off + uint32(batch)*16
			c.nativeStructAlloc.Cursor = uint32(batch)
		}
		b.StartTimer()
		if err := c.CollectFull(roots); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if c.Stats().LiveObjects != 65536 {
			b.Fatal("dense refill lost objects")
		}
		c.Close()
		b.StartTimer()
	}
}
