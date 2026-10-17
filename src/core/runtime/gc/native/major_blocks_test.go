//go:build amd64

package gc

import (
	"fmt"
	"testing"
)

func TestMajorBlocksDenseSparseTransitions(t *testing.T) {
	d, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, VerifyAfterCollect: true}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const count = 8192
	roots := make(RefSliceRoots, count)
	for i := range roots {
		roots[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if len(c.majorBlocks) != 0 || cap(c.majorBlocks) != 0 {
		t.Fatal("dense heap allocated a block map")
	}
	if err := c.CollectFull(roots[:1]); err != nil {
		t.Fatal(err)
	}
	if len(c.majorBlocks) != 1 || c.majorBlocks[0] != ^uint64(0) {
		t.Fatal("first sparse sweep did not enable a conservative map")
	}
	if err := c.CollectFull(roots[:1]); err != nil {
		t.Fatal(err)
	}
	if c.majorBlocks[0] != 1 || c.Stats().LiveObjects != 1 {
		t.Fatal("indexed sweep did not prune empty blocks")
	}
	for i := 1; i < len(roots); i++ {
		roots[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if len(c.majorBlocks) != 0 || cap(c.majorBlocks) == 0 || c.Stats().LiveObjects != count {
		t.Fatal("dense transition lost objects or retained the active map")
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if len(c.majorBlocks) != 1 || c.majorBlocks[0] != ^uint64(0) {
		t.Fatal("re-enabled map depended on stale occupancy")
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.majorBlocks[0] != 0 || c.Stats().LiveObjects != 0 {
		t.Fatal("empty transition retained a live block")
	}
}

func TestMajorBlocksReusedAndGrowingHandles(t *testing.T) {
	d, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 2 << 20, VerifyAfterCollect: true}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 16383; i++ {
		if _, err = c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	if err = c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if len(c.majorBlocks) == 0 {
		t.Fatal("map not initialized")
	}
	// First sparse collection enables a conservative all-set index. A second
	// full sweep proves empty blocks before later collections can skip them.
	if err = c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	for _, word := range c.majorBlocks {
		if word != 0 {
			t.Fatal("empty block retained")
		}
	}
	for _, count := range []int{1, 512, 17000} {
		roots := make(RefSliceRoots, count)
		for i := range roots {
			roots[i], err = c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err = c.CollectFull(roots); err != nil {
			t.Fatal(err)
		}
		if c.Stats().LiveObjects != uint32(count) {
			t.Fatal("lost live object")
		}
		if err = c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		if c.Stats().LiveObjects != 0 {
			t.Fatal("reused or grown block was skipped")
		}
	}
}

func TestMajorBlocksNativeReservations(t *testing.T) {
	d, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fresh := range []bool{false, true} {
		c, err := NewCollector(Config{NurseryBytes: 1 << 20, VerifyAfterCollect: true}, []TypeDesc{d})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 16383; i++ {
			if _, err = c.NewStructDefault(0); err != nil {
				t.Fatal(err)
			}
		}
		if err = c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		// Prune the initial all-set index so reservation coverage must set bits.
		if err = c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		for _, word := range c.majorBlocks {
			if word != 0 {
				t.Fatal("initial map not pruned")
			}
		}
		if !fresh {
			for i := range c.freeHandles {
				c.freeHandles[i] = uint32((i*257)%16383 + 1)
			}
		}
		if fresh {
			c.freeHandles = nil
		} // retire free identities to force table growth
		if !c.PrepareNativeStructAllocation(16) {
			t.Fatal("reservation failed")
		}
		if !c.majorYoungDirty {
			t.Fatal("native reservation did not dirty the nursery index")
		}
		// Publish one object using the same metadata order as native allocation.
		h := c.nativeStructAlloc.Handles[0]
		off := c.nurseryBump
		c.handles[h] = handleEntry{off: off, size: 16, space: spaceNursery}
		c.writeHeader(makeObjRef(h), ObjHeader{TypeID: 0, Size: 16, Flags: FlagPointerFree})
		c.nurseryBump = off + 16
		c.nativeStructAlloc.Cursor = 1
		if err = c.CollectFull(RefSliceRoots{makeObjRef(h)}); err != nil {
			t.Fatal(err)
		}
		if c.Stats().LiveObjects != 1 {
			t.Fatal("native object lost")
		}
		if err = c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		if c.Stats().LiveObjects != 0 {
			t.Fatal("native dead object skipped")
		}
		if !c.PrepareNativeStructAllocation(16) {
			t.Fatal("second reservation failed")
		}
		c.CancelNativeAllocationBatch()
		if err = c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		for _, word := range c.majorBlocks {
			if word != 0 {
				t.Fatal("canceled block retained")
			}
		}
		c.Close()
		if c.majorBlocks != nil {
			t.Fatal("closed map retained")
		}
	}
}

// Prove newly added words stay indexed until dead objects in them are swept.
func TestMajorBlocksGrowingWordReclaimsDead(t *testing.T) {
	c := newMajorBlocksEmptyCollector(t, 16383, false)
	free := c.freeHandles
	c.freeHandles = nil
	ref, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	c.freeHandles = free
	h := uint32(ref) >> 1
	if h != 16384 || len(c.majorBlocks) != 2 || c.majorBlocks[1] != 1 {
		t.Fatalf("new word not indexed: h=%d map=%x", h, c.majorBlocks)
	}
	if err = c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if len(c.majorBlocks) == 0 || c.handles[h].space != spaceFree || c.Stats().LiveObjects != 0 {
		t.Fatal("growing word skipped dead object or disabled index")
	}
}

func newMajorBlocksEmptyCollector(t *testing.T, count int, checked bool) *Collector {
	t.Helper()
	plain, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	array, err := NewArrayDesc(2, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 2 << 20, LargeObjectBytes: 4096, ThroughputHeapBytes: 8 << 20, PoisonFreed: true, VerifyAfterCollect: true}, []TypeDesc{plain, refs, array})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if checked {
		if err = c.EnableCheckedHandles(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < count; i++ {
		if _, err = c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err = c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.majorBlocks) == 0 {
		t.Fatal("index disabled")
	}
	for _, word := range c.majorBlocks {
		if word != 0 {
			t.Fatal("index not empty")
		}
	}
	return c
}

func TestMajorBlocksOldLargeAndRemembered(t *testing.T) {
	for _, checked := range []bool{false, true} {
		t.Run(fmt.Sprint(checked), func(t *testing.T) {
			c := newMajorBlocksEmptyCollector(t, 16383, checked)
			c.tenuringThreshold = 1
			parent, err := c.NewStructDefault(1)
			if err != nil {
				t.Fatal(err)
			}
			if err = c.CollectMinor(RefSliceRoots{parent}); err != nil {
				t.Fatal(err)
			}
			if c.handles[uint32(parent)>>1].space != spaceOld {
				t.Fatal("parent not promoted")
			}
			child, err := c.NewArrayDefault(2, 4096)
			if err != nil {
				t.Fatal(err)
			}
			if c.handles[uint32(child)>>1].space != spaceLarge {
				t.Fatal("child not large")
			}
			if err = c.StructSet(parent, 0, RefValue(child)); err != nil {
				t.Fatal(err)
			}
			if len(c.remembered) == 0 {
				t.Fatal("old-young edge not remembered")
			}
			if err = c.CollectFull(RefSliceRoots{parent}); err != nil {
				t.Fatal(err)
			}
			if len(c.majorBlocks) == 0 || c.Stats().LiveObjects != 2 {
				t.Fatal("lost graph or disabled index")
			}
			if err = c.CollectFull(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			if len(c.majorBlocks) == 0 || c.Stats().LiveObjects != 0 || len(c.remembered) != 0 {
				t.Fatal("dead old/large graph not reclaimed")
			}
			if !c.PrepareNativeArrayAllocationImmediate(NativeArrayAllocMaxBytes + 16) {
				t.Fatal("large reservation failed")
			}
			h := c.nativeLargeArrayAlloc.Handle
			if c.majorBlocks[h>>majorWordShift]&(uint64(1)<<((h>>majorBlockShift)&63)) == 0 {
				t.Fatal("large reservation omitted")
			}
			if err = c.CollectFull(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			for _, word := range c.majorBlocks {
				if word != 0 {
					t.Fatal("canceled large reservation kept block")
				}
			}
		})
	}
}
