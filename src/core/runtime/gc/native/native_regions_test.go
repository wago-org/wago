package gc

import "testing"

func TestNativeRegionsCancellationAndRefill(t *testing.T) {
	c := regionCollector(t)
	live := regionStruct(t, c, 0, EmptyRoots{})
	roots := Slots{(*Root)(&live)}
	for range 1000 {
		if !c.PrepareNativeStructAllocation(32) {
			t.Fatal("reservation refused")
		}
		s := c.nativeStructAlloc
		if s.ChunkStart != 32 || s.ChunkEnd != 64 || s.Count != nativeArrayHandleBatch {
			t.Fatalf("reservation %+v", s)
		}
		for _, h := range s.Handles[:s.Count] {
			if c.handles[h].space != spaceFree {
				t.Fatal("uninitialized identity exposed")
			}
		}
		c.CancelNativeAllocationBatch()
		if c.edenRegions[0].bump != 32 || c.nativeAllocEpoch == s.Epoch || len(c.nurseryHandles) != 1 {
			t.Fatal("unused reservation not canceled")
		}
	}
	if len(c.handles) > nativeArrayHandleBatch+2 {
		t.Fatal("cancellation leaked handles")
	}
	// Simulate the native publication protocol. Guest tests exercise the actual
	// generated instructions; this test inspects internal ownership precisely.
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
	if c.edenRegions[0].bump != 64 || len(c.nurseryHandles) != 2 {
		t.Fatal("partial batch lost publication")
	}
	if !c.PrepareNativeAllocation(32) || c.nativeStructAlloc.ChunkStart != 64 {
		t.Fatal("refill did not advance regions")
	}
	epoch := c.nativeAllocEpoch
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.nativeAllocEpoch == epoch || c.nativeStructAlloc.Count != 0 || c.edenRegions[1].bump != 0 {
		t.Fatal("collection retained reservation")
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

func TestNativeRegionsGrowthAndFallback(t *testing.T) {
	c := regionCollector(t)
	if !c.PrepareNativeStructAllocation(24) {
		t.Fatal("reservation refused")
	}
	epoch := c.nativeAllocEpoch
	d, err := NewStructDesc(TypeID(len(c.types)), []StorageKind{StorageI64})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddTypes([]TypeDesc{d}); err != nil {
		t.Fatal(err)
	}
	if epoch == c.nativeAllocEpoch || c.nativeStructAlloc.Count != 0 || c.edenRegions[0].bump != 0 {
		t.Fatal("growth did not invalidate reservation")
	}
	for _, size := range []uint32{0, 65, 256, 4096, ^uint32(0)} {
		if c.PrepareNativeStructAllocation(size) {
			t.Fatalf("unsupported struct size %d", size)
		}
	}
	if c.PrepareNativeDeadStructAllocation(32) || c.PrepareNativeArrayAllocationImmediate(512) || c.PrepareDeadAllocationBatch(32, 8).TryReserve(1) {
		t.Fatal("unsafe writer admitted")
	}
	before := c.Stats().Allocations
	if err := c.ReserveDeadStructAllocations(0, 100, EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().Allocations-before != 100 {
		t.Fatal("checked dead charges lost")
	}
	if err := c.Verify(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
}
