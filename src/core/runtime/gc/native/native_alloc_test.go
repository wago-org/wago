package gc

import (
	"fmt"
	"reflect"
	"slices"
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

func TestReserveNativeHandlesHonorsRequestedBatch(t *testing.T) {
	if nativeStructHandleBatch <= nativeArrayHandleBatch {
		return
	}
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.reserveNativeHandles(nativeArrayHandleBatch)
	for i, h := range c.nativeStructAlloc.Handles[nativeArrayHandleBatch:] {
		if h != 0 {
			t.Fatalf("unused handle %d = %d, want zero", nativeArrayHandleBatch+i, h)
		}
	}
}

func TestNativeLargeArrayAllocStateLayout(t *testing.T) {
	var s nativeLargeArrayAllocState
	if unsafe.Sizeof(s) != NativeLargeArrayAllocStateSize ||
		unsafe.Offsetof(s.Epoch) != NativeStructAllocEpochOffset ||
		unsafe.Offsetof(s.Cursor) != NativeStructAllocCursorOffset ||
		unsafe.Offsetof(s.Count) != NativeStructAllocCountOffset ||
		unsafe.Offsetof(s.HandleBase) != NativeStructAllocHandleBaseOffset ||
		unsafe.Offsetof(s.ChunkStart) != NativeStructAllocChunkStartOffset ||
		unsafe.Offsetof(s.ChunkCursor) != NativeStructAllocChunkCursorOffset ||
		unsafe.Offsetof(s.ChunkEnd) != NativeStructAllocChunkEndOffset ||
		unsafe.Offsetof(s.ChunkBump) != NativeStructAllocChunkBumpOffset ||
		unsafe.Offsetof(s.Handle) != NativeStructAllocHandlesOffset || unsafe.Offsetof(s.Rewind) != 36 {
		t.Fatalf("native large-array state layout changed: size=%d state=%+v", unsafe.Sizeof(s), s)
	}
}

func TestNativeLargeArrayAllocationSurvivesStructRefill(t *testing.T) {
	array, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	strct, err := NewStructDesc(1, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{array, strct})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const bytes = 16 << 10
	if !c.PrepareNativeArrayAllocation(bytes) {
		t.Fatal("large array reservation was not prepared")
	}
	want := c.nativeLargeArrayAlloc
	if want.Count != 1 || want.HandleBase == 0 || want.ChunkEnd-want.ChunkStart != bytes {
		t.Fatalf("large array state = %+v", want)
	}
	if !c.PrepareNativeStructAllocation(24) {
		t.Fatal("struct batch was not prepared")
	}
	if c.nativeLargeArrayAlloc != want {
		t.Fatalf("struct refill changed large array state: got %+v want %+v", c.nativeLargeArrayAlloc, want)
	}
	if c.nurseryBump != want.ChunkEnd {
		t.Fatalf("nursery bump = %d, want %d", c.nurseryBump, want.ChunkEnd)
	}
}

func TestNativeLargeArrayAllocationCanceledAtCollection(t *testing.T) {
	array, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{array})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.PrepareNativeArrayAllocation(16 << 10) {
		t.Fatal("large array reservation was not prepared")
	}
	h := c.nativeLargeArrayAlloc.Handle
	epoch := c.nativeLargeArrayEpoch
	if err := c.CollectMinor(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.nativeLargeArrayAlloc.Count != 0 || c.nativeLargeArrayEpoch == epoch || c.nurseryBump != 0 || len(c.nurseryHandles) != 0 {
		t.Fatalf("post-collection state = %+v epoch=%d nursery bump=%d nursery=%v", c.nativeLargeArrayAlloc, c.nativeLargeArrayEpoch, c.nurseryBump, c.nurseryHandles)
	}
	if len(c.freeHandles) == 0 || c.freeHandles[len(c.freeHandles)-1] != h {
		t.Fatalf("reserved handle %d was not recycled: %v", h, c.freeHandles)
	}
}

func TestNativeThroughputArrayReservationCanceledAtCollection(t *testing.T) {
	array, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{array})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const bytes = 40 << 10
	if !c.PrepareNativeArrayAllocation(bytes) {
		t.Fatal("throughput array reservation was not prepared")
	}
	if space := spaceKind((c.nativeLargeArrayAlloc.ChunkBump >> 16) & 0xff); space != spaceLarge {
		t.Fatalf("reserved space = %d, want large", space)
	}
	if c.throughput.bump < bytes {
		t.Fatalf("throughput bump = %d, want at least %d", c.throughput.bump, bytes)
	}
	if err := c.CollectMinor(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.throughput.bump != 0 || c.nativeLargeArrayAlloc.Count != 0 || len(c.nurseryHandles) != 0 {
		t.Fatalf("canceled throughput reservation = state %+v bump %d nursery %v", c.nativeLargeArrayAlloc, c.throughput.bump, c.nurseryHandles)
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
	c.handles[h] = handleEntry{size: sz, space: spaceNursery}
	c.writeHeader(makeObjRef(h), ObjHeader{TypeID: uint32(desc.ID), Size: sz, Flags: FlagPointerFree})
	s.ChunkCursor = sz
	s.Cursor = 1
	if got := c.Stats().Allocations; got != 1 || c.stats.Allocations != 0 {
		t.Fatalf("active batch allocations = %d stored %d, want 1 stored 0", got, c.stats.Allocations)
	}
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
	if got := c.Stats().Allocations; got != 1 || c.stats.Allocations != 1 {
		t.Fatalf("committed batch allocations = %d stored %d, want 1", got, c.stats.Allocations)
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
	h := c.newHandle(handleEntry{off: 0, size: 24, space: spaceNursery})
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
	if c.nativeStructAlloc.Count != nativeArrayHandleBatch || c.nativeStructAlloc.ChunkEnd == 0 {
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

func TestNativeArrayAllocationKeepsPrimingAcrossRefills(t *testing.T) {
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
		if c.PrepareNativeArrayAllocation(80) {
			t.Fatalf("cold allocation %d reserved a batch", i)
		}
	}
	for refill := 0; refill < 4; refill++ {
		if !c.PrepareNativeArrayAllocation(80) {
			t.Fatalf("primed refill %d repeated the cold delay", refill)
		}
		c.CancelNativeAllocationBatch()
		if _, err := c.NewArray(0, 16, I32Value(int32(refill))); err != nil {
			t.Fatal(err)
		}
	}
	for _, collect := range []func(RootSet) error{c.CollectMinor, c.CollectFull} {
		if err := collect(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		for i := uint8(1); i < nativeArrayGenericRefillThreshold; i++ {
			if c.PrepareNativeArrayAllocation(80) {
				t.Fatalf("post-collection allocation %d bypassed the cold delay", i)
			}
		}
		if !c.PrepareNativeArrayAllocation(80) {
			t.Fatal("post-collection priming did not reserve a batch")
		}
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
	c.handles[first] = handleEntry{off: 0, size: 24, space: spaceNursery}
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
	h := c.newHandle(handleEntry{size: 24, space: spaceNursery})
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
		h := c.newHandle(handleEntry{size: 16, space: spaceNursery})
		c.nurseryHandles = append(c.nurseryHandles, h)
		prefix = append(prefix, h)
	}
	if !c.PrepareNativeStructHandles() {
		t.Fatal("native handle batch was not prepared")
	}
	s := &c.nativeStructAlloc
	consumed := s.Handles[0]
	c.handles[consumed] = handleEntry{size: 16, space: spaceNursery}
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

func TestNativeStructHandleCancellationPreservesDeadChargesAfterPromotion(t *testing.T) {
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

	size, err := StructSize(desc)
	if err != nil {
		t.Fatal(err)
	}
	s := &c.nativeStructAlloc
	h := s.Handles[0]
	c.handles[h] = handleEntry{size: size, space: spaceNursery}
	c.writeHeader(makeObjRef(h), ObjHeader{TypeID: uint32(desc.ID), Size: size, Flags: FlagPointerFree})
	s.Cursor = 1
	c.nurseryBump = 4 * size
	c.stats.Allocations = 4
	c.stats.LiveObjects = 3
	if err := c.ForcePromote(makeObjRef(h)); err != nil {
		t.Fatal(err)
	}

	before := c.nurseryBump
	c.CancelNativeAllocationBatch()
	if c.nurseryBump != before {
		t.Fatalf("cancel discarded dead allocation charge: bump %d -> %d", before, c.nurseryBump)
	}
	if got := c.Stats().LiveObjects; got != 4 {
		t.Fatalf("live objects after cancel = %d, want 4", got)
	}
	if err := c.CollectMinor(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.nurseryBump != 0 || c.Stats().LiveObjects != 1 {
		t.Fatalf("minor collection state = bump %d live %d, want 0/1", c.nurseryBump, c.Stats().LiveObjects)
	}
}

func TestDeadNurseryAccountingDoesNotAliasAfterUint32CollectionCycle(t *testing.T) {
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.stats.LiveObjects = 3
	c.nativeView.setDeadNurseryCycle(0)
	if got := c.Stats().LiveObjects; got != 3 {
		t.Fatalf("initial live objects = %d, want 3", got)
	}
	c.stats.MinorCollections = 1 << 32
	if got := c.Stats().LiveObjects; got != 0 {
		t.Fatalf("stale live objects after 2^32 collections = %d, want 0", got)
	}
	if c.hasPendingDeadNurseryObjects() {
		t.Fatal("stale dead nursery accounting remained pending after 2^32 collections")
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
		h := c.newHandle(handleEntry{size: 16, space: spaceNursery})
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

// The native batch must preserve the general allocator's reuse order and all
// parallel state, including checked identities that retire on exhaustion.
func TestNativeHandleBatchMatchesGeneralAllocator(t *testing.T) {
	for _, checked := range []bool{false, true} {
		for _, freeCount := range []int{0, 7, 31, 32, 47} {
			t.Run(fmt.Sprintf("checked=%t/free=%d", checked, freeCount), func(t *testing.T) {
				makeCollector := func() *Collector {
					c, err := NewCollector(Config{}, nil)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(c.Close)
					if checked {
						if err := c.EnableCheckedHandles(); err != nil {
							t.Fatal(err)
						}
					}
					for range 64 {
						c.newHandle(handleEntry{})
					}
					// Spare capacity may retain old data. Fresh identities must clear
					// it while reused identities retain their existing side-table state.
					entries, marks, colors := len(c.handles), len(c.mark), len(c.tinyGC.color)
					c.handles = slices.Grow(c.handles, 32)[:entries+32]
					for i := entries; i < len(c.handles); i++ {
						c.handles[i] = handleEntry{space: spaceOld, size: 80}
					}
					c.handles = c.handles[:entries]
					c.mark = slices.Grow(c.mark, 32)[:marks+32]
					for i := range c.mark {
						c.mark[i] = true
					}
					c.mark = c.mark[:marks]
					c.tinyGC.color = slices.Grow(c.tinyGC.color, 32)[:colors+32]
					for i := range c.tinyGC.color {
						c.tinyGC.color[i] = 37
					}
					c.tinyGC.color = c.tinyGC.color[:colors]
					for i := range freeCount {
						c.freeHandles = append(c.freeHandles, uint32(1+(i*17)%64))
					}
					if checked && freeCount != 0 {
						generations := *c.checkedHandles
						generations[c.freeHandles[freeCount-1]] = ^uint64(0)
						if freeCount > 1 {
							generations[c.freeHandles[freeCount-2]] = ^uint64(0) - 1
						}
					}
					return c
				}
				got, want := makeCollector(), makeCollector()
				var expected [nativeStructHandleBatch]uint32
				for i := range expected {
					expected[i] = want.newHandle(handleEntry{})
				}
				got.reserveNativeHandles(nativeStructHandleBatch)
				if got.nativeStructAlloc.Handles != expected {
					t.Fatalf("batch = %v, want %v", got.nativeStructAlloc.Handles, expected)
				}
				for name, pair := range map[string][2]any{
					"entries":     {got.handles, want.handles},
					"free":        {got.freeHandles, want.freeHandles},
					"marks":       {got.mark, want.mark},
					"colors":      {got.tinyGC.color, want.tinyGC.color},
					"generations": {got.checkedHandles, want.checkedHandles},
				} {
					if !reflect.DeepEqual(pair[0], pair[1]) {
						t.Fatalf("%s differ: got %v want %v", name, pair[0], pair[1])
					}
				}
				if !reflect.DeepEqual(got.nurseryHandles, expected[:]) {
					t.Fatalf("dense nursery = %v, want %v", got.nurseryHandles, expected)
				}
			})
		}
	}
}
