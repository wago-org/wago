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
	if s.Cursor != 0 || s.Count != nativeStructHandleBatch || s.HandleBase == 0 || s.ChunkStart != 0 || s.ChunkCursor != 0 || s.ChunkEnd != 0 || len(c.nurseryHandles) != nativeStructHandleBatch || c.nurseryBump != 0 || c.stats.Allocations != 0 {
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

func TestNativeArrayAllocationPrimesBeforeReserving(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := uint8(1); i < nativeArrayGenericRefillThreshold; i++ {
		if c.PrepareNativeArrayAllocation(24) {
			t.Fatalf("array allocation %d unexpectedly reserved a native batch", i)
		}
		if c.arraySlow != i || c.nativeStructAlloc.Count != 0 || c.nativeStructAlloc.ChunkEnd != 0 || c.nurseryBump != 0 {
			t.Fatalf("array allocation %d did not remain reservation-free", i)
		}
		c.CancelNativeAllocationBatch()
		if c.arraySlow != i {
			t.Fatalf("helper cancellation cleared slow count %d", i)
		}
	}
	if !c.PrepareNativeArrayAllocation(24) {
		t.Fatalf("array allocation %d did not reserve a native batch", nativeArrayGenericRefillThreshold)
	}
	if c.nativeStructAlloc.Count != nativeStructHandleBatch || c.nativeStructAlloc.ChunkEnd == 0 {
		t.Fatal("second array allocation left no usable native batch")
	}
	c.discardNativeStructHandles()
	if c.arraySlow != 0 {
		t.Fatal("collecting-boundary cancellation retained the array slow count")
	}
	if !c.PrepareNativeArrayAllocationImmediate(24) {
		t.Fatal("persistent-boundary product did not reserve immediately")
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

func TestNativeStructHandleCancellationPreservesDenseNurseryPrefix(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const prefixCount = 4096
	prefix := make([]uint32, 0, prefixCount)
	for range prefixCount {
		h := c.newHandle(handleEntry{size: 16, allocSize: 16, space: spaceNursery})
		c.nurseryHandles = append(c.nurseryHandles, h)
		prefix = append(prefix, h)
	}
	if !c.PrepareNativeStructHandles() {
		t.Fatal("native handle batch was not prepared")
	}
	s := &c.nativeStructAlloc
	consumed := s.Handles[0]
	c.handles[consumed] = handleEntry{size: 16, allocSize: 16, space: spaceNursery}
	s.Cursor = 1
	c.CancelNativeAllocationBatch()
	if len(c.nurseryHandles) != prefixCount+1 {
		t.Fatalf("nursery handles = %d, want %d", len(c.nurseryHandles), prefixCount+1)
	}
	for i, want := range prefix {
		if got := c.nurseryHandles[i]; got != want {
			t.Fatalf("nursery prefix %d = %d, want %d", i, got, want)
		}
	}
	if got := c.nurseryHandles[prefixCount]; got != consumed {
		t.Fatalf("consumed native handle = %d, want %d", got, consumed)
	}
}

func BenchmarkNativeAllocationBatchCancelDenseNursery(b *testing.B) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	for range 1 << 16 {
		h := c.newHandle(handleEntry{size: 16, allocSize: 16, space: spaceNursery})
		c.nurseryHandles = append(c.nurseryHandles, h)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if !c.PrepareNativeStructHandles() {
			b.Fatal("native handle batch was not prepared")
		}
		c.CancelNativeAllocationBatch()
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
