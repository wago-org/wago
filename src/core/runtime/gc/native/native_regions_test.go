package gc

import "testing"

func TestConfiguredMatureRegionsUseContiguousNativeReservations(t *testing.T) {
	c := regionCollector(t)
	live := regionStruct(t, c, 0, EmptyRoots{})
	roots := Slots{(*Root)(&live)}

	for range 1000 {
		before := c.nurseryBump
		if !c.PrepareNativeAllocation(32) {
			t.Fatal("reservation refused")
		}
		s := c.nativeStructAlloc
		if s.ChunkStart != before || s.ChunkEnd <= s.ChunkStart || s.Count != nativeArrayHandleBatch {
			t.Fatalf("reservation %+v before=%d", s, before)
		}
		for _, h := range s.Handles[:s.Count] {
			if c.handles[h].space != spaceFree {
				t.Fatal("uninitialized identity exposed")
			}
		}
		c.CancelNativeAllocationBatch()
		if c.nurseryBump != before || c.nativeAllocEpoch == s.Epoch || len(c.nurseryHandles) != 1 {
			t.Fatal("unused contiguous reservation not canceled exactly")
		}
	}
	if len(c.handles) > nativeArrayHandleBatch+2 {
		t.Fatal("cancellation leaked handles")
	}

	if !c.PrepareNativeAllocation(32) {
		t.Fatal("array reservation refused")
	}
	s := &c.nativeStructAlloc
	h := s.Handles[0]
	r := makeObjRef(h)
	c.handles[h] = handleEntry{off: s.ChunkCursor, size: 32, space: spaceNursery}
	c.writeHeader(r, ObjHeader{TypeID: 0, Size: 32, Flags: FlagPointerFree})
	s.ChunkCursor += 32
	s.Cursor++
	c.stats.Allocations++
	c.CancelNativeAllocationBatch()
	if c.nurseryBump != 64 || len(c.nurseryHandles) != 2 {
		t.Fatalf("partial batch lost publication: bump=%d nursery=%v", c.nurseryBump, c.nurseryHandles)
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredMatureRegionsPreserveNativeLargeReservationAndCancellation(t *testing.T) {
	c, err := NewCollector(Config{NurseryBytes: 2048, NurseryRegionShift: 10, LargeObjectBytes: 4096}, testTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.PrepareNativeArrayAllocationImmediate(512) {
		t.Fatal("large contiguous reservation refused")
	}
	s := c.nativeLargeArrayAlloc
	if s.Count != 1 || s.ChunkStart != 0 || s.ChunkEnd != 512 || c.nurseryBump != 512 {
		t.Fatalf("large reservation %+v bump=%d", s, c.nurseryBump)
	}
	c.discardNativeLargeArrayAllocation()
	if c.nativeLargeArrayAlloc.Count != 0 || c.nurseryBump != 0 {
		t.Fatal("large reservation cancellation did not restore Eden")
	}
}
