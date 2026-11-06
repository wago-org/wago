package gc

import (
	"fmt"
	"testing"
)

// Pair the contiguous native reservation protocol with every mature-descriptor
// width. Checked LOS must invalidate an outstanding small chunk.
func TestRollupReservationPolicyCycles(t *testing.T) {
	for _, shift := range []uint8{0, 6, 12} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			c := newTestCollector(t, Config{NurseryBytes: 16384, NurseryRegionShift: shift, ThroughputHeapBytes: 1 << 20, ThroughputPageBytes: 4096, LargeObjectBytes: 256, VerifyAfterCollect: true})
			for cycle := 0; cycle < 20; cycle++ {
				if !c.PrepareNativeAllocation(32) {
					t.Fatal("small reservation refused")
				}
				s := &c.nativeStructAlloc
				h := s.Handles[s.Cursor]
				live := makeObjRef(h)
				c.handles[h] = handleEntry{off: s.ChunkCursor, size: 32, space: spaceNursery}
				c.writeHeader(live, ObjHeader{TypeID: 0, Size: 32, Flags: FlagPointerFree})
				s.ChunkCursor += 32
				s.Cursor++
				c.stats.Allocations++
				if err := c.StructSet(live, 1, I64Value(int64(cycle))); err != nil {
					t.Fatal(err)
				}
				roots := Slots{(*Root)(&live)}
				epoch := c.nativeAllocEpoch
				_ = c.PrepareNativeArrayAllocationImmediate(8208)
				large, err := c.NewArrayDefaultWithRoots(2, 2048, roots)
				if err != nil {
					t.Fatal(err)
				}
				roots = append(roots, (*Root)(&large))
				if c.nativeStructAlloc.Count != 0 || epoch == c.nativeAllocEpoch {
					t.Fatal("LOS retained small reservation")
				}
				if !c.PrepareNativeAllocation(32) {
					t.Fatal("small refill refused")
				}
				c.CancelNativeAllocationBatch()
				c.CancelNativeAllocationBatch()
				if err = c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
				value, err := c.StructGet(live, 1)
				if err != nil || value.I64() != int64(cycle) {
					t.Fatal("partial batch payload", err)
				}
				if err = c.Verify(roots); err != nil {
					t.Fatal(err)
				}
				if err = c.CollectFull(EmptyRoots{}); err != nil {
					t.Fatal(err)
				}
			}
			if !c.PrepareNativeAllocation(32) {
				t.Fatal("final reserve")
			}
			desc, err := NewStructDesc(TypeID(len(c.types)), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = c.AddTypes([]TypeDesc{desc}); err != nil {
				t.Fatal(err)
			}
			if c.nativeStructAlloc.Count == 0 {
				t.Fatal("metadata growth discarded type-independent reservation")
			}
			c.Close()
			c.Close()
		})
	}
}
