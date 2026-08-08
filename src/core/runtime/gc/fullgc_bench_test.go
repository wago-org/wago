package gc

import (
	"fmt"
	"testing"
)

func newFullGCBenchmarkCollector(b *testing.B, disableMoving bool) *Collector {
	b.Helper()
	d, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{
		NurseryBytes:         16 << 20,
		ThroughputHeapBytes:  64 << 20,
		ThroughputPageBytes:  64 << 10,
		DisableMovingNursery: disableMoving,
	}, []TypeDesc{d})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	return c
}

// BenchmarkThroughputFullLivePointerFree isolates repeated high-survival full
// collections without allocation/setup noise. It guards the cheap pointer-free
// case where a small fixed metadata cost can otherwise look like a large
// percentage regression in the allocation-per-cycle matrix.
func BenchmarkThroughputFullLivePointerFree(b *testing.B) {
	fields := []StorageKind{
		StorageI64, StorageI64, StorageI64, StorageI64,
		StorageI64, StorageI64, StorageI64, StorageI64,
	}
	d, err := NewStructDesc(0, fields)
	if err != nil {
		b.Fatal(err)
	}
	for _, objects := range []int{90, 900, 9000} {
		b.Run(fmt.Sprintf("objects=%d", objects), func(b *testing.B) {
			c, err := NewCollector(Config{NurseryBytes: 16 << 20, ThroughputHeapBytes: 64 << 20}, []TypeDesc{d})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(c.Close)
			values := make([]Root, objects)
			roots := make(Slots, objects)
			for i := range roots {
				r, err := c.NewStructDefault(0)
				if err != nil {
					b.Fatal(err)
				}
				values[i] = Root(r)
				roots[i] = &values[i]
			}
			if err := c.CollectFull(roots); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ReportMetric(float64(objects), "live-objects/op")
			b.ResetTimer()
			for range b.N {
				if err := c.CollectFull(roots); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkThroughputFullOldHeapPause isolates the major-collection pause for
// old objects. Allocation, promotion, validation, and cleanup remain outside
// the timer so lazy free-span indexing cannot disguise pause movement as setup.
func BenchmarkThroughputFullOldHeapPause(b *testing.B) {
	for _, objects := range []int{1000, 10_000} {
		for _, survival := range []int{0, 50} {
			b.Run(fmt.Sprintf("objects=%d/survival=%d", objects, survival), func(b *testing.B) {
				c := newFullGCBenchmarkCollector(b, false)
				live := objects * survival / 100
				values := make([]Root, live)
				roots := make(Slots, live)
				for i := range roots {
					roots[i] = &values[i]
				}
				b.ReportMetric(float64(objects), "old-objects/op")
				b.ResetTimer()
				for range b.N {
					b.StopTimer()
					for i := range objects {
						r, err := c.NewStructDefault(0)
						if err != nil {
							b.Fatal(err)
						}
						if err := c.ForcePromote(r); err != nil {
							b.Fatal(err)
						}
						if i < live {
							values[i] = Root(r)
						}
					}
					b.StartTimer()
					if err := c.CollectFull(roots); err != nil {
						b.Fatal(err)
					}
					b.StopTimer()
					if got := c.Stats().LiveObjects; got != uint32(live) {
						b.Fatalf("live=%d, want %d", got, live)
					}
					for i := range values {
						values[i] = Root(Null())
					}
					if err := c.CollectFull(nil); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
				}
			})
		}
	}
}

// BenchmarkThroughputFullOldHeapAmortized includes the following old-space
// refill so deferred indexing debt is charged to the collection that created
// it rather than reported as a free pause reduction.
func BenchmarkThroughputFullOldHeapAmortized(b *testing.B) {
	for _, objects := range []int{1000, 10_000} {
		b.Run(fmt.Sprintf("objects=%d", objects), func(b *testing.B) {
			c := newFullGCBenchmarkCollector(b, false)
			fill := func() {
				for range objects {
					r, err := c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					if err := c.ForcePromote(r); err != nil {
						b.Fatal(err)
					}
				}
			}
			fill()
			b.ReportAllocs()
			b.ReportMetric(float64(objects), "old-objects/op")
			b.ResetTimer()
			for range b.N {
				if err := c.CollectFull(nil); err != nil {
					b.Fatal(err)
				}
				fill()
			}
		})
	}
}

// BenchmarkThroughputFullOldHeapMinorAmortized uses the production batch
// promotion path after each full collection. Immediate tenuring keeps the main
// and candidate policies semantically identical for branch comparisons.
func BenchmarkThroughputFullOldHeapMinorAmortized(b *testing.B) {
	for _, objects := range []int{1000, 10_000} {
		b.Run(fmt.Sprintf("objects=%d", objects), func(b *testing.B) {
			c := newFullGCBenchmarkCollector(b, true)
			values := make([]Root, objects)
			roots := make(Slots, objects)
			for i := range roots {
				roots[i] = &values[i]
			}
			fill := func() {
				for i := range values {
					r, err := c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					values[i] = Root(r)
				}
				if err := c.CollectMinor(roots); err != nil {
					b.Fatal(err)
				}
			}
			fill()
			b.ReportAllocs()
			b.ReportMetric(float64(objects), "old-objects/op")
			b.ResetTimer()
			for range b.N {
				for i := range values {
					values[i] = Root(Null())
				}
				if err := c.CollectFull(nil); err != nil {
					b.Fatal(err)
				}
				fill()
			}
		})
	}
}
