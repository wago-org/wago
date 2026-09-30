package gc

import (
	"encoding/binary"
	"testing"
)

// BenchmarkGCBarrierStateMatrix isolates the parent states required by #315.
// The unremembered-old case includes bounded remembered/card metadata creation;
// its reset is deliberately part of each operation because that cold transition
// is the behavior being measured. Other cases remain steady-state hot paths.
func BenchmarkGCBarrierStateMatrix(b *testing.B) {
	for _, tc := range []struct {
		name    string
		cfg     Config
		length  uint32
		prepare func(*Collector, Ref, Ref) error
		reset   func(*Collector, Ref)
	}{
		{name: "nursery-parent", cfg: Config{StressNurseryBytes: 1 << 20}, length: 64},
		{name: "remembered-old-parent", cfg: Config{StressNurseryBytes: 1 << 20}, length: 64, prepare: func(c *Collector, parent, child Ref) error {
			if err := c.ForcePromote(parent); err != nil {
				return err
			}
			return c.ArraySet(parent, 0, RefValue(child))
		}},
		{name: "unremembered-old-parent", cfg: Config{StressNurseryBytes: 1 << 20}, length: 64, prepare: func(c *Collector, parent, _ Ref) error {
			return c.ForcePromote(parent)
		}, reset: func(c *Collector, parent Ref) {
			h := handleOf(parent)
			c.handles[h].remembered = false
			c.handles[h].cardSlot = 0
			c.remembered = c.remembered[:0]
			c.objectCards = c.objectCards[:0]
			c.freeObjectCardSlot = 0
			c.cardFallback = false
		}},
		{name: "large-parent", cfg: Config{StressNurseryBytes: 1 << 20, LargeObjectBytes: 256}, length: 128, prepare: func(c *Collector, parent, child Ref) error {
			if err := c.ForcePromote(parent); err != nil {
				return err
			}
			return c.ArraySet(parent, 0, RefValue(child))
		}},
		{name: "tiny-parent", cfg: Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, length: 64},
		{name: "tiny-active-marked-child", cfg: Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, length: 64, prepare: func(c *Collector, parent, child Ref) error {
			c.tinyGC.state = tinyMark
			c.tinySetColor(handleOf(parent), tinyBlack)
			c.tinySetColor(handleOf(child), tinyBlack)
			return nil
		}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			leaf, err := NewStructDesc(0, nil)
			if err != nil {
				b.Fatal(err)
			}
			refs, err := NewArrayDesc(1, StorageRefNull)
			if err != nil {
				b.Fatal(err)
			}
			c, err := NewCollector(tc.cfg, []TypeDesc{leaf, refs})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(c.Close)
			parent, err := c.NewArrayDefault(1, tc.length)
			if err != nil {
				b.Fatal(err)
			}
			child, err := c.NewStructDefault(0)
			if err != nil {
				b.Fatal(err)
			}
			if tc.prepare != nil {
				if err := tc.prepare(c, parent, child); err != nil {
					b.Fatal(err)
				}
			}
			value := RefValue(child)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if tc.reset != nil {
					tc.reset(c, parent)
				}
				if err := c.ArraySet(parent, uint32(i)%tc.length, value); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(c.RememberedCount()), "remembered")
			b.ReportMetric(float64(c.CardCount()), "cards")
		})
	}
}

// BenchmarkTinyIncrementalBarrierMarkedChild isolates the common active-cycle
// post-write barrier when the child is already marked in the current epoch. It
// guards the epoch lookup independently from object-access validation and stores.
func BenchmarkTinyIncrementalBarrierMarkedChild(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf, refs})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	parent, err := c.NewArrayDefault(1, 1)
	if err != nil {
		b.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		b.Fatal(err)
	}
	c.tinyGC.state = tinyMark
	c.tinySetColor(handleOf(parent), tinyBlack)
	c.tinySetColor(handleOf(child), tinyBlack)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.writeBarrierObjectRange(parent, child, 0, 3)
	}
}

// BenchmarkTinyBarrierPolicy compares the retained incremental-update fast path
// with the minimum SATB overwrite work: load the deleted edge and test/queue it.
// A complete SATB implementation would additionally require this pre-barrier on
// every scalar and bulk overwrite.
func BenchmarkTinyBarrierPolicy(b *testing.B) {
	leaf, _ := NewStructDesc(0, nil)
	refs, _ := NewArrayDesc(1, StorageRefNull)
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf, refs})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	parent, _ := c.NewArrayDefault(1, 1)
	child, _ := c.NewStructDefault(0)
	if err := c.ArraySet(parent, 0, RefValue(child)); err != nil {
		b.Fatal(err)
	}
	c.tinyGC.state = tinyMark
	c.tinySetBlack(handleOf(parent))
	c.tinySetBlack(handleOf(child))
	b.Run("incremental-update", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			c.tinyWriteBarrierObject(parent, child)
		}
	})
	b.Run("satb-delete", func(b *testing.B) {
		payload := c.bytes(parent)[PayloadOffset:]
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			old := Ref(binary.LittleEndian.Uint32(payload))
			c.tinyMarkRef(old)
		}
	})
}

func BenchmarkRememberedArrayWrite(b *testing.B) {
	for _, profile := range []struct {
		name string
		cfg  Config
	}{
		{name: "throughput", cfg: Config{StressNurseryBytes: 1 << 20}},
		{name: "tiny", cfg: Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 8}},
	} {
		b.Run(profile.name, func(b *testing.B) {
			obj, _ := NewStructDesc(0, nil)
			i32, _ := NewArrayDesc(1, StorageI32)
			i64, _ := NewArrayDesc(2, StorageI64)
			refs, _ := NewArrayDesc(3, StorageRefNull)
			c, err := NewCollector(profile.cfg, []TypeDesc{obj, i32, i64, refs})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(c.Close)
			array, err := c.NewArrayDefault(3, 4096)
			if err != nil {
				b.Fatal(err)
			}
			if err := c.ForcePromote(array); err != nil {
				b.Fatal(err)
			}
			child, err := c.NewStructDefault(0)
			if err != nil {
				b.Fatal(err)
			}
			value := RefValue(child)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.ArraySet(array, uint32(i)&4095, value); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(c.RememberedCount()), "remembered")
			b.ReportMetric(float64(c.CardCount()), "cards")
		})
	}
}
