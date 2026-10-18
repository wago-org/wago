//go:build amd64 && !tinygo

package gc

import (
	"fmt"
	"testing"
)

func TestDeadCohortBulkRecycling(t *testing.T) {
	for _, poison := range []bool{false, true} {
		for _, count := range []int{1, 64, 512, 4096} {
			t.Run(fmt.Sprintf("%d/poison%v", count, poison), func(t *testing.T) {
				leaf, err := NewStructDesc(0, []StorageKind{StorageI32})
				if err != nil {
					t.Fatal(err)
				}
				array, err := NewArrayDesc(1, StorageI32)
				if err != nil {
					t.Fatal(err)
				}
				c, err := NewCollector(Config{NurseryBytes: 4 << 20, ThroughputHeapBytes: 16 << 20, LargeObjectBytes: 512, PoisonFreed: poison, VerifyAfterCollect: true}, []TypeDesc{leaf, array})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				old, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				c.NewGlobalSlot(old)
				if err = c.StructSet(old, 0, I32Value(173)); err != nil {
					t.Fatal(err)
				}
				if err = c.ForcePromote(old); err != nil {
					t.Fatal(err)
				}
				large, err := c.NewArrayDefault(1, 1024)
				if err != nil {
					t.Fatal(err)
				}
				dead := []Ref{large}
				for range count {
					ref, err := c.NewStructDefault(0)
					if err != nil {
						t.Fatal(err)
					}
					dead = append(dead, ref)
				}
				if !c.PrepareNativeStructHandles() {
					t.Fatal("missing native reservation")
				}
				freeBefore := len(c.freeHandles) + int(c.nativeStructAlloc.Count-c.nativeStructAlloc.Cursor)
				entries := len(c.handles)
				if err = c.CollectMinor(EmptyRoots{}); err != nil {
					t.Fatal(err)
				}
				if len(c.freeHandles) != freeBefore+len(dead) {
					t.Fatalf("free%d want%d", len(c.freeHandles), freeBefore+len(dead))
				}
				seen := make(map[uint32]bool, len(c.freeHandles))
				for _, h := range c.freeHandles {
					if h == 0 || int(h) >= len(c.handles) || seen[h] || h == handleOf(old) {
						t.Fatalf("invalid/duplicate/old handle%d", h)
					}
					seen[h] = true
				}
				for _, ref := range dead {
					if !seen[handleOf(ref)] || c.entry(ref).space != spaceFree {
						t.Fatalf("dead ref%d not recycled", ref)
					}
				}
				if *c.entry(large) != (handleEntry{}) {
					t.Fatal("large owner was not released")
				}
				got, err := c.StructGet(old, 0)
				if err != nil || got.I32() != 173 {
					t.Fatalf("old value%v err%v", got, err)
				}
				for range count {
					if _, err := c.NewStructDefault(0); err != nil {
						t.Fatal(err)
					}
				}
				if len(c.handles) != entries {
					t.Fatal("recycling grew handle table")
				}
				if err = c.CollectMinor(EmptyRoots{}); err != nil {
					t.Fatal(err)
				}
				if err = c.Verify(EmptyRoots{}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// Includes restoring the dead cohort plus cleanup, with metadata preallocated.
// The cycle benchmark below measures real allocation and collection separately.
func BenchmarkDeadCohortCleanup(b *testing.B) {
	for _, n := range []int{512, 4096, 65536} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			d, err := NewStructDesc(0, nil)
			if err != nil {
				b.Fatal(err)
			}
			c, err := NewCollector(Config{NurseryBytes: 4 << 20}, []TypeDesc{d})
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			for range n {
				if _, err = c.NewStructDefault(0); err != nil {
					b.Fatal(err)
				}
			}
			nursery := append([]uint32(nil), c.nurseryHandles...)
			if len(nursery) != n {
				b.Fatal("fixture collected before measurement")
			}
			c.freeHandles = make([]uint32, 0, n+1)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for _, h := range nursery {
					c.handles[h].space = spaceNursery
				}
				c.nurseryHandles = nursery
				c.freeHandles = c.freeHandles[:0]
				if !c.finishDeadNurseryEvacuation() || len(c.freeHandles) != n {
					b.Fatal("cleanup failed")
				}
			}
		})
	}
}

func BenchmarkDeadCohortCycle(b *testing.B) {
	for _, n := range []int{512, 4096, 65536} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			d, err := NewStructDesc(0, nil)
			if err != nil {
				b.Fatal(err)
			}
			c, err := NewCollector(Config{NurseryBytes: 4 << 20}, []TypeDesc{d})
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			for range 2 {
				for range n {
					if _, err = c.NewStructDefault(0); err != nil {
						b.Fatal(err)
					}
				}
				if err = c.CollectMinor(EmptyRoots{}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for range n {
					if _, err = c.NewStructDefault(0); err != nil {
						b.Fatal(err)
					}
				}
				if err = c.CollectMinor(EmptyRoots{}); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(cap(c.freeHandles)*4), "free-cap-B")
		})
	}
}
