package gc

import (
	"fmt"
	"slices"
	"testing"
	"unsafe"
)

func TestThroughputBackingGrowthIsGeometricAfterFirstPage(t *testing.T) {
	h := throughputHeap{pageBytes: 64 << 10, limit: 4 << 20}
	h.mem = makeAlignedBytes(512<<10, 16)
	if err := h.growBacking(576 << 10); err != nil {
		t.Fatal(err)
	}
	if got := len(h.mem); got != 768<<10 {
		t.Fatalf("small backing growth = %d, want %d", got, 768<<10)
	}
	h.mem = makeAlignedBytes(1<<20, 16)
	if err := h.growBacking((1 << 20) + (64 << 10)); err != nil {
		t.Fatal(err)
	}
	if got := len(h.mem); got != 1536<<10 {
		t.Fatalf("geometric backing growth = %d, want %d", got, 1536<<10)
	}
}

func TestProfileNormalization(t *testing.T) {
	c := newTestCollector(t, Config{})
	if c.cfg.Profile != ProfileThroughput || c.cfg.Allocator != AllocatorPagedSizeClass || c.cfg.Runtime != RuntimeGenerational {
		t.Fatalf("zero config normalized to %+v", c.cfg)
	}
	tiny := newTinyTestCollector(t, Config{Profile: ProfileTiny})
	if tiny.cfg.Allocator != AllocatorTinyFixedBlock || tiny.cfg.Runtime != RuntimeIncrementalMarkSweep {
		t.Fatalf("tiny config normalized to %+v", tiny.cfg)
	}
	if _, err := NewCollector(Config{Profile: ProfileTiny, Allocator: AllocatorPagedSizeClass, Runtime: RuntimeIncrementalMarkSweep}, testTypes(t)); err == nil {
		t.Fatal("expected invalid tiny allocator/runtime combination rejection")
	}
	if _, err := NewCollector(Config{Profile: ProfileThroughput, Allocator: AllocatorTinyFixedBlock, Runtime: RuntimeGenerational}, testTypes(t)); err == nil {
		t.Fatal("expected invalid throughput allocator/runtime combination rejection")
	}
	if _, err := NewCollector(Config{Profile: ProfileTiny, DisableCollection: true}, testTypes(t)); err == nil {
		t.Fatal("expected collection-disabled tiny profile rejection")
	}
}

func TestCollectionDisabledHeapIsBoundedAndStable(t *testing.T) {
	c := newTestCollector(t, Config{
		DisableCollection:    true,
		ThroughputHeapBytes:  4096,
		ThroughputPageBytes:  4096,
		ThroughputClassLimit: 4096,
	})
	first, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(first, 0, I32Value(77)); err != nil {
		t.Fatal(err)
	}
	allocations := 1
	for {
		_, err = c.NewStructDefault(0)
		if err != nil {
			break
		}
		allocations++
	}
	if allocations == 0 || err == nil {
		t.Fatalf("collection-disabled allocations=%d err=%v", allocations, err)
	}
	if got, getErr := c.StructGet(first, 0); getErr != nil || got.I32() != 77 {
		t.Fatalf("first object after exhaustion = %v, %v; want 77, nil", got, getErr)
	}
	stats := c.Stats()
	if stats.MinorCollections != 0 || stats.FullCollections != 0 {
		t.Fatalf("collection-disabled collector ran minor/full collections %d/%d", stats.MinorCollections, stats.FullCollections)
	}
}

func TestThroughputFreeSpanMetadataIsReused(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096}); err != nil {
		t.Fatal(err)
	}
	free, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.alloc(32, spaceOld); err != nil { // keep the reusable span below bump
		t.Fatal(err)
	}
	if err := h.free(free); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1024; i++ {
		e, err := h.alloc(32, spaceOld)
		if err != nil {
			t.Fatal(err)
		}
		if e.off != free.off {
			t.Fatalf("reused offset = %d, want %d", e.off, free.off)
		}
		if err := h.free(e); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(h.spanNodes); got != 1 {
		t.Fatalf("throughput span metadata grew to %d nodes, want one reusable node", got)
	}
	if err := h.verify(nil); err != nil {
		t.Fatal(err)
	}
}

func TestThroughputOldSpaceReuseAfterFullGC(t *testing.T) {
	c := newTestCollector(t, Config{StressNurseryBytes: 96, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	a, _ := c.NewStructDefault(0)
	root := Root(a)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	off := c.entry(a).off
	root = Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	b, _ := c.NewStructDefault(0)
	root = Root(b)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(b).off != off {
		t.Fatalf("old space not reused: got off %d want %d", c.entry(b).off, off)
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}

func TestThroughputLargeObjectReuse(t *testing.T) {
	c := newTestCollector(t, Config{LargeObjectBytes: 64, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	a, err := c.NewArray(2, 32, I32Value(1))
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(a).space != spaceLarge {
		t.Fatal("array was not large")
	}
	off := c.entry(a).off
	root := Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	b, err := c.NewArray(2, 32, I32Value(2))
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(b).off != off {
		t.Fatalf("large space not reused: got off %d want %d", c.entry(b).off, off)
	}
}

func TestThroughputOversizedNurseryObjectUsesLargeSpace(t *testing.T) {
	c := newTestCollector(t, Config{StressNurseryBytes: 64, LargeObjectBytes: 256, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	arr, err := c.NewArray(2, 16, I32Value(7)) // 16-byte header + 16*4-byte payload > 64-byte nursery.
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(arr).space != spaceLarge {
		t.Fatalf("oversized nursery object space=%v, want large", c.entry(arr).space)
	}
	if got, err := c.ArrayGet(arr, 15); err != nil || got.I32() != 7 {
		t.Fatalf("array element = %v, %v; want 7, nil", got, err)
	}
	if err := c.Verify(nil); err != nil {
		t.Fatal(err)
	}
}

func TestThroughputClassLimitMustBeSupportedSizeClass(t *testing.T) {
	for _, limit := range []uint32{0, 32, 4096, 32768} {
		c, err := NewCollector(Config{ThroughputClassLimit: limit}, testTypes(t))
		if err != nil {
			t.Fatalf("supported class limit %d rejected: %v", limit, err)
		}
		c.Close()
	}
	for _, limit := range []uint32{16, 33, 4097, 65536} {
		if _, err := NewCollector(Config{ThroughputClassLimit: limit}, testTypes(t)); err == nil {
			t.Fatalf("unsupported class limit %d accepted", limit)
		}
	}
}

func linearThroughputClassFor(size, limit uint32) int {
	for i, classSize := range throughputClassSizes {
		if size <= classSize && classSize <= limit {
			return i
		}
	}
	return -1
}

func TestThroughputClassForMatchesLinearReference(t *testing.T) {
	limits := make([]uint32, 1, len(throughputClassSizes)+1)
	for _, limit := range throughputClassSizes {
		limits = append(limits, limit)
	}
	maxSize := throughputClassSizes[len(throughputClassSizes)-1] + 1
	for _, limit := range limits {
		h := throughputHeap{classLimit: limit}
		for size := uint32(0); size <= maxSize; size++ {
			want := linearThroughputClassFor(size, limit)
			if got := h.classFor(size); got != want {
				t.Fatalf("classFor(size=%d, limit=%d) = %d, want %d", size, limit, got, want)
			}
		}
	}

	boundarySizes := []uint32{0, 1, 15, 16, 17, ^uint32(0) - 15, ^uint32(0)}
	for _, classSize := range throughputClassSizes {
		boundarySizes = append(boundarySizes, classSize-1, classSize, classSize+1)
	}
	for limit := uint32(0); limit <= throughputClassSizes[len(throughputClassSizes)-1]+1; limit++ {
		h := throughputHeap{classLimit: limit}
		for _, size := range boundarySizes {
			want := linearThroughputClassFor(size, limit)
			if got := h.classFor(size); got != want {
				t.Fatalf("classFor(size=%d, malformed limit=%d) = %d, want %d", size, limit, got, want)
			}
		}
	}
}

var benchmarkThroughputClass int

//go:noinline
func benchmarkThroughputClassFor(h *throughputHeap, size uint32) int {
	return h.classFor(size)
}

func BenchmarkThroughputClassFor(b *testing.B) {
	for _, tc := range []struct {
		name  string
		size  uint32
		limit uint32
	}{
		{name: "small", size: 32, limit: 4096},
		{name: "common", size: 96, limit: 4096},
		{name: "middle", size: 768, limit: 4096},
		{name: "default-limit", size: 4096, limit: 4096},
		{name: "maximum-limit", size: 32768, limit: 32768},
	} {
		b.Run(tc.name, func(b *testing.B) {
			h := throughputHeap{classLimit: tc.limit}
			got := 0
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				got += benchmarkThroughputClassFor(&h, tc.size)
			}
			benchmarkThroughputClass = got
		})
	}
	b.Run("mixed", func(b *testing.B) {
		h := throughputHeap{classLimit: 32768}
		index, got := 0, 0
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			got += benchmarkThroughputClassFor(&h, throughputClassSizes[index])
			index++
			if index == len(throughputClassSizes) {
				index = 0
			}
		}
		benchmarkThroughputClass = got + index
	})
}

var benchmarkThroughputLargeSpan uint32

func newThroughputLargeSpanFixture(spans int, fitting bool) throughputHeap {
	memBytes := uint32(spans*128 + 512)
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: memBytes, ThroughputPageBytes: 4096, ThroughputClassLimit: 32}); err != nil {
		panic(err)
	}
	h.mem = makeAlignedBytes(memBytes, 16)
	h.bump = memBytes
	for i := 0; i < spans; i++ {
		if err := h.insertFreeSpan(throughputFreeSpan{off: uint32(i * 128), size: 32}); err != nil {
			panic(err)
		}
	}
	if fitting {
		if err := h.insertFreeSpan(throughputFreeSpan{off: uint32(spans * 128), size: 128}); err != nil {
			panic(err)
		}
	}
	return h
}

func churnThroughputLargeSpan(h *throughputHeap) error {
	entry, err := h.alloc(64, spaceLarge)
	if err != nil {
		return err
	}
	return h.free(entry)
}

func TestThroughputPrefixChurnRestoresExactIndexState(t *testing.T) {
	h := newThroughputLargeSpanFixture(64, true)
	root, freeHead := h.spanRoot, h.freeNodeHead
	nodes := slices.Clone(h.spanNodes)
	spans := h.freeSpans()
	freeBytes, spanCount := h.freeBytes, h.spanCount
	if err := churnThroughputLargeSpan(&h); err != nil {
		t.Fatal(err)
	}
	if h.spanRoot != root || h.freeNodeHead != freeHead || h.freeBytes != freeBytes || h.spanCount != spanCount ||
		!slices.Equal(h.spanNodes, nodes) || !slices.Equal(h.freeSpans(), spans) {
		t.Fatal("prefix allocation and one-neighbor coalescing did not restore exact indexed state")
	}
	if err := h.verify(nil); err != nil {
		t.Fatal(err)
	}
}

//go:noinline
func benchmarkFindThroughputLargeSpan(h *throughputHeap, size uint32) (uint32, uint32) {
	return h.findSpanCounted(size)
}

func TestThroughputFreeSpanIndexTracksMutations(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 32}); err != nil {
		t.Fatal(err)
	}
	h.mem = makeAlignedBytes(4096, 16)
	h.bump = 320
	if err := h.insertFreeSpan(throughputFreeSpan{off: 0, size: 64}); err != nil {
		t.Fatal(err)
	}
	if err := h.insertFreeSpan(throughputFreeSpan{off: 128, size: 128}); err != nil {
		t.Fatal(err)
	}
	if h.largestFree() != 128 || h.freeBytes != 192 || h.spanCount != 2 {
		t.Fatalf("initial summary largest=%d free=%d spans=%d", h.largestFree(), h.freeBytes, h.spanCount)
	}
	entry, err := h.alloc(96, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if entry.off != 128 || h.largestFree() != 64 || h.freeBytes != 96 || h.spanCount != 2 {
		t.Fatalf("after allocation entry=%+v largest=%d free=%d spans=%d", entry, h.largestFree(), h.freeBytes, h.spanCount)
	}
	if err := h.free(entry); err != nil {
		t.Fatal(err)
	}
	if h.largestFree() != 128 || h.freeBytes != 192 || h.spanCount != 2 {
		t.Fatalf("after free largest=%d free=%d spans=%d", h.largestFree(), h.freeBytes, h.spanCount)
	}
	if err := h.verify(nil); err != nil {
		t.Fatal(err)
	}
}

func TestThroughputFreeSpanCoalescesAcrossAllocationClasses(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 128}); err != nil {
		t.Fatal(err)
	}
	first, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.alloc(96, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.alloc(64, spaceLarge); err != nil { // keep both frees below bump
		t.Fatal(err)
	}
	if err := h.free(first); err != nil {
		t.Fatal(err)
	}
	if err := h.free(second); err != nil {
		t.Fatal(err)
	}
	spans := h.freeSpans()
	if len(spans) != 1 || spans[0].off != 0 || spans[0].size != 128 {
		t.Fatalf("coalesced spans = %v, want [0,128)", spans)
	}
	large, err := h.alloc(128, spaceLarge)
	if err != nil || large.off != 0 {
		t.Fatalf("large allocation from class frees = %+v, %v", large, err)
	}
}

func TestThroughputPromotionDestinationsAreGroupedBySizeClass(t *testing.T) {
	small, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	mediumFields := make([]StorageKind, 6)
	for i := range mediumFields {
		mediumFields[i] = StorageI64
	}
	medium, err := NewStructDesc(1, mediumFields)
	if err != nil {
		t.Fatal(err)
	}
	largeFields := make([]StorageKind, 20)
	for i := range largeFields {
		largeFields[i] = StorageI64
	}
	large, err := NewStructDesc(2, largeFields)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{NurseryBytes: 4096, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}, []TypeDesc{small, medium, large})
	largeRef, err := c.NewStructDefault(2)
	if err != nil {
		t.Fatal(err)
	}
	smallRef, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	mediumRef, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	roots := []Root{Root(largeRef), Root(smallRef), Root(mediumRef)}
	if err := c.CollectMinor(stressRootSlots(roots)); err != nil {
		t.Fatal(err)
	}
	largeOff := c.entry(Ref(roots[0])).off
	smallOff := c.entry(Ref(roots[1])).off
	mediumOff := c.entry(Ref(roots[2])).off
	if !(smallOff < mediumOff && mediumOff < largeOff) {
		t.Fatalf("promotion offsets small=%d medium=%d large=%d are not size-grouped", smallOff, mediumOff, largeOff)
	}
}

func TestThroughputFullCollectionDefersSpanIndexingUntilAllocation(t *testing.T) {
	c := newTestCollector(t, Config{StressNurseryBytes: 64, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	roots := make([]Root, 2)
	for i := range roots {
		object, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		roots[i] = Root(object)
	}
	if err := c.CollectMinor(stressRootSlots(roots)); err != nil {
		t.Fatal(err)
	}
	oldBump := c.throughput.bump
	for i := range roots {
		roots[i] = Root(Null())
	}
	if err := c.CollectFull(stressRootSlots(roots)); err != nil {
		t.Fatal(err)
	}
	if len(c.throughput.pendingFree) != 2 || c.throughput.pendingBytes == 0 || c.throughput.spanCount != 0 || c.throughput.bump != oldBump {
		t.Fatalf("deferred sweep pending=%d bytes=%d spans=%d bump=%d/%d", len(c.throughput.pendingFree), c.throughput.pendingBytes, c.throughput.spanCount, c.throughput.bump, oldBump)
	}
	e, err := c.throughput.alloc(64, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if e.off != 0 || len(c.throughput.pendingFree) != 0 || c.throughput.pendingBytes != 0 {
		t.Fatalf("allocation after deferred sweep entry=%+v pending=%d bytes=%d", e, len(c.throughput.pendingFree), c.throughput.pendingBytes)
	}
}

func TestThroughputAllocRunPreservesFirstFitAndTail(t *testing.T) {
	newHeap := func(t *testing.T) throughputHeap {
		t.Helper()
		var h throughputHeap
		if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096}); err != nil {
			t.Fatal(err)
		}
		h.mem = makeAlignedBytes(4096, 16)
		h.bump = 256
		return h
	}
	t.Run("first-fit-too-small", func(t *testing.T) {
		h := newHeap(t)
		if err := h.insertFreeSpan(throughputFreeSpan{off: 0, size: 32}); err != nil {
			t.Fatal(err)
		}
		if err := h.insertFreeSpan(throughputFreeSpan{off: 64, size: 128}); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := h.tryAllocRun(32, 3, spaceOld); err != nil || ok {
			t.Fatalf("run bypassed smaller first fit: ok=%t err=%v", ok, err)
		}
	})
	t.Run("consume-short-tail", func(t *testing.T) {
		h := newHeap(t)
		if err := h.insertFreeSpan(throughputFreeSpan{off: 0, size: 112}); err != nil {
			t.Fatal(err)
		}
		run, ok, err := h.tryAllocRun(32, 3, spaceOld)
		if err != nil || !ok {
			t.Fatalf("run allocation ok=%t err=%v", ok, err)
		}
		if run.off != 0 || run.allocSize != 32 || run.lastAllocSize != 48 || h.spanCount != 0 || h.freeBytes != 0 {
			t.Fatalf("run=%+v spans=%d free=%d", run, h.spanCount, h.freeBytes)
		}
	})
	t.Run("warmed-bump", func(t *testing.T) {
		h := newHeap(t)
		h.bump = 0
		run, ok, err := h.tryAllocRun(32, 3, spaceOld)
		if err != nil || !ok || run.off != 0 || h.bump != 96 {
			t.Fatalf("warmed bump run=%+v ok=%t err=%v bump=%d", run, ok, err, h.bump)
		}
	})
}

func TestThroughputPendingContiguousRunsCoalesceInEitherOrder(t *testing.T) {
	for _, descending := range []bool{false, true} {
		name := "ascending"
		if descending {
			name = "descending"
		}
		t.Run(name, func(t *testing.T) {
			var h throughputHeap
			if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096}); err != nil {
				t.Fatal(err)
			}
			entries := make([]handleEntry, 3)
			for i := range entries {
				var err error
				entries[i], err = h.alloc(64, spaceOld)
				if err != nil {
					t.Fatal(err)
				}
			}
			if descending {
				slices.Reverse(entries)
			}
			for _, e := range entries {
				if err := h.deferFree(e); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.sweepAllPending(); err != nil {
				t.Fatal(err)
			}
			if h.bump != 0 || len(h.pendingFree) != 0 || h.pendingBytes != 0 || h.spanCount != 0 || h.freeBytes != 0 {
				t.Fatalf("coalesced pending state bump=%d pending=%d/%d spans=%d free=%d", h.bump, len(h.pendingFree), h.pendingBytes, h.spanCount, h.freeBytes)
			}
		})
	}
}

func TestThroughputTopFreeRewindsBump(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096}); err != nil {
		t.Fatal(err)
	}
	first, err := h.alloc(64, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.alloc(64, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.free(first); err != nil {
		t.Fatal(err)
	}
	if err := h.free(second); err != nil {
		t.Fatal(err)
	}
	if h.bump != 0 || h.spanCount != 0 || h.freeBytes != 0 {
		t.Fatalf("top reclamation bump=%d spans=%d free=%d", h.bump, h.spanCount, h.freeBytes)
	}
	reused, err := h.alloc(128, spaceLarge)
	if err != nil || reused.off != 0 {
		t.Fatalf("bump reuse = %+v, %v", reused, err)
	}
}

func TestThroughputFreeSpanFootprint(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("64-bit fixed-footprint assertion")
	}
	if got := unsafe.Sizeof(throughputSpanNode{}); got != 28 {
		t.Fatalf("throughputSpanNode size = %d, want 28", got)
	}
	if got := unsafe.Sizeof(throughputHeap{}); got != 120 {
		t.Fatalf("throughputHeap size = %d, want 120", got)
	}
}

func BenchmarkThroughputFreshBump(b *testing.B) {
	const limit = uint32(64 << 10)
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: limit, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096}); err != nil {
		b.Fatal(err)
	}
	h.mem = makeAlignedBytes(limit, 16)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if h.bump == limit {
			h.bump = 0
		}
		if _, err := h.alloc(32, spaceOld); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkThroughputCommonSpanReuse(b *testing.B) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096}); err != nil {
		b.Fatal(err)
	}
	h.mem = makeAlignedBytes(4096, 16)
	h.bump = 64
	if err := h.insertFreeSpan(throughputFreeSpan{off: 0, size: 32}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e, err := h.alloc(32, spaceOld)
		if err != nil {
			b.Fatal(err)
		}
		if err := h.free(e); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkThroughputLargeSpanMiss(b *testing.B) {
	for _, spans := range []int{64, 1024, 16384} {
		b.Run(fmt.Sprintf("spans=%d", spans), func(b *testing.B) {
			h := newThroughputLargeSpanFixture(spans, false)
			var found, steps uint32
			found = throughputNoSlot
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				idx, n := benchmarkFindThroughputLargeSpan(&h, 64)
				found = idx
				steps += n
			}
			b.StopTimer()
			benchmarkThroughputLargeSpan = found
			if found != throughputNoSlot {
				b.Fatalf("miss result=%d", found)
			}
			b.ReportMetric(float64(steps)/float64(b.N), "search-steps/op")
		})
	}
}

func BenchmarkThroughputLargeSpanChurn(b *testing.B) {
	for _, spans := range []int{64, 1024, 16384} {
		b.Run(fmt.Sprintf("spans=%d", spans), func(b *testing.B) {
			h := newThroughputLargeSpanFixture(spans, true)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := churnThroughputLargeSpan(&h); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if err := h.verify(nil); err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(h.spanCount), "free-spans")
			b.ReportMetric(float64(h.freeBytes), "free-bytes")
			if h.freeBytes != 0 {
				b.ReportMetric(100*(1-float64(h.largestFree())/float64(h.freeBytes)), "fragmentation-percent")
			}
			b.ReportMetric(float64(len(h.spanNodes))*float64(unsafe.Sizeof(throughputSpanNode{})), "metadata-bytes")
		})
	}
}

func TestThroughputAllocatorFragmentationReuse(t *testing.T) {
	c := newTestCollector(t, Config{LargeObjectBytes: 64, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
	a, _ := c.NewArray(2, 16, I32Value(1))
	b, _ := c.NewArray(2, 16, I32Value(1))
	offA := c.entry(a).off
	c.free(handleOf(a))
	c.free(handleOf(b))
	x, err := c.NewArray(2, 32, I32Value(3))
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(x).off != offA {
		// Coalesced large free spans should be reused before growing.
		t.Fatalf("large coalesced span not reused, off=%d", c.entry(x).off)
	}
}

func TestThroughputVerifyCatchesInvalidMetadata(t *testing.T) {
	c := newTestCollector(t, Config{})
	r, _ := c.NewStructDefault(0)
	root := Root(r)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	c.handles[handleOf(r)].off = uint32(len(c.throughput.mem)) + 8
	if err := c.Verify(Slots{&root}); err == nil {
		t.Fatal("expected verify to reject out-of-bounds throughput handle")
	}
}
