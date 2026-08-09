package gc

import (
	"testing"
	"unsafe"
)

func TestNativeStructAllocStateLayout(t *testing.T) {
	var s nativeStructAllocState
	if unsafe.Sizeof(s) != NativeStructAllocStateSize ||
		unsafe.Offsetof(s.Epoch) != NativeStructAllocEpochOffset ||
		unsafe.Offsetof(s.Cursor) != NativeStructAllocCursorOffset ||
		unsafe.Offsetof(s.Count) != NativeStructAllocCountOffset ||
		unsafe.Offsetof(s.HandleBase) != NativeStructAllocHandleBaseOffset ||
		unsafe.Offsetof(s.ChunkStart) != NativeStructAllocChunkStartOffset ||
		unsafe.Offsetof(s.ChunkCursor) != NativeStructAllocChunkCursorOffset ||
		unsafe.Offsetof(s.ChunkEnd) != NativeStructAllocChunkEndOffset ||
		unsafe.Offsetof(s.ChunkBump) != NativeStructAllocChunkBumpOffset ||
		unsafe.Offsetof(s.Handles) != NativeStructAllocHandlesOffset {
		t.Fatalf("native allocation state layout changed: size=%d epoch=%d cursor=%d count=%d base=%d chunk=%d/%d/%d bump=%d handles=%d",
			unsafe.Sizeof(s), unsafe.Offsetof(s.Epoch), unsafe.Offsetof(s.Cursor), unsafe.Offsetof(s.Count), unsafe.Offsetof(s.HandleBase),
			unsafe.Offsetof(s.ChunkStart), unsafe.Offsetof(s.ChunkCursor), unsafe.Offsetof(s.ChunkEnd), unsafe.Offsetof(s.ChunkBump), unsafe.Offsetof(s.Handles))
	}
}

func TestNativeStructHandleBatchReservationAndCollection(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.PrepareNativeStructHandles() {
		t.Fatal("native handle batch was not prepared")
	}
	s := &c.nativeStructAlloc
	if s.Cursor != 0 || s.Count != nativeStructHandleBatch || s.HandleBase == 0 || s.ChunkStart != 0 || s.ChunkCursor != 0 || s.ChunkEnd != NativeAllocationChunkBytes || len(c.nurseryHandles) != nativeStructHandleBatch || c.nurseryBump != NativeAllocationChunkBytes || c.stats.Allocations != 0 {
		t.Fatalf("reserved state = cursor %d count %d base %d chunk %d/%d/%d nursery handles %d bump %d allocations %d", s.Cursor, s.Count, s.HandleBase, s.ChunkStart, s.ChunkCursor, s.ChunkEnd, len(c.nurseryHandles), c.nurseryBump, c.stats.Allocations)
	}
	seen := make(map[uint32]bool, nativeStructHandleBatch)
	for _, h := range s.Handles {
		if h == 0 || seen[h] || int(h) >= len(c.handles) || c.handles[h].space != spaceFree {
			t.Fatalf("invalid reserved handle %d", h)
		}
		seen[h] = true
	}
	startHandles := len(c.handles)
	if !c.PrepareNativeStructHandles() || len(c.handles) != startHandles {
		t.Fatal("preparing a live batch replaced its reservations")
	}

	// Simulate one fully committed native object. The remaining identities are
	// unpublished and must be recycled before collection traces the nursery.
	sz, err := StructSize(desc)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handles[0]
	c.handles[h] = handleEntry{size: sz, allocSize: sz, space: spaceNursery}
	c.writeHeader(makeObjRef(h), ObjHeader{TypeID: uint32(desc.ID), Size: sz, Flags: FlagPointerFree})
	s.ChunkCursor = sz
	s.Cursor = 1
	c.stats.Allocations++
	epoch := c.nativeAllocEpoch
	if err := c.CollectMinor(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if s.Cursor != 0 || s.Count != 0 || c.nativeAllocEpoch == epoch || len(c.nurseryHandles) != 0 || c.nurseryBump != 0 {
		t.Fatalf("post-collection state = cursor %d count %d epoch %d/%d nursery handles %d bump %d", s.Cursor, s.Count, c.nativeAllocEpoch, epoch, len(c.nurseryHandles), c.nurseryBump)
	}
	if len(c.freeHandles) != nativeStructHandleBatch {
		t.Fatalf("recycled handles = %d, want %d", len(c.freeHandles), nativeStructHandleBatch)
	}
	if !c.PrepareNativeStructHandles() || len(c.handles) != startHandles {
		t.Fatalf("reused batch grew handle table: %d -> %d", startHandles, len(c.handles))
	}
}

func TestNativeAllocationUnusedAlignedChunkRestoresExactBump(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	h := c.newHandle(handleEntry{off: 0, size: 24, allocSize: 24, space: spaceNursery})
	c.nurseryHandles = append(c.nurseryHandles, h)
	c.nurseryBump = 24
	if !c.PrepareNativeAllocation(24) {
		t.Fatal("native allocation batch was not prepared")
	}
	c.CancelNativeAllocationBatch()
	if c.nurseryBump != 24 {
		t.Fatalf("canceled aligned chunk bump = %d, want 24", c.nurseryBump)
	}
}

func TestNativeAllocationRejectsOversizedChunk(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.PrepareNativeAllocation(NativeAllocationChunkBytes + 1) {
		t.Fatal("oversized native allocation unexpectedly reserved a chunk")
	}
	if c.nativeStructAlloc.Count != 0 || c.nativeStructAlloc.ChunkEnd != 0 || c.nurseryBump != 0 {
		t.Fatal("oversized native allocation mutated collector state")
	}
}

func TestNativeAllocationChunkCanceledBeforeGoAllocation(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.PrepareNativeAllocation(24) {
		t.Fatal("native allocation batch was not prepared")
	}
	s := &c.nativeStructAlloc
	first := s.HandleBase
	c.handles[first] = handleEntry{off: 0, size: 24, allocSize: 24, space: spaceNursery}
	c.writeHeader(makeObjRef(first), ObjHeader{TypeID: uint32(desc.ID), Size: 24, Flags: FlagPointerFree})
	s.Cursor = 1
	s.ChunkCursor = 24
	c.stats.Allocations++

	second, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.handles[handleOf(second)].off; got != 24 {
		t.Fatalf("Go slow path left a reserved chunk gap: offset=%d, want 24", got)
	}
	if s.Count != 0 || s.ChunkEnd != 0 {
		t.Fatalf("Go slow path retained native state: %+v", *s)
	}
}

func TestNativeStructHandleCancellationCannotDuplicateNurserySet(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.PrepareNativeStructHandles() {
		t.Fatal("native handle batch was not prepared")
	}
	c.discardNativeStructHandles()
	if len(c.nurseryHandles) != 0 {
		t.Fatalf("canceled nursery set retains %d handles", len(c.nurseryHandles))
	}
	h := c.newHandle(handleEntry{size: 24, allocSize: 24, space: spaceNursery})
	c.nurseryHandles = append(c.nurseryHandles, h)
	count := 0
	for _, candidate := range c.nurseryHandles {
		if candidate == h {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reused handle %d occurs %d times in nursery set", h, count)
	}
}

func TestNativeStructHandleBatchRejectedProfiles(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []Config{{DisableCollection: true}, {CollectEveryAlloc: true}, {Profile: ProfileTiny}} {
		c, err := NewCollector(cfg, []TypeDesc{desc})
		if err != nil {
			t.Fatal(err)
		}
		if c.PrepareNativeStructHandles() {
			t.Fatalf("prepared native handles for config %+v", cfg)
		}
		c.Close()
	}
}
