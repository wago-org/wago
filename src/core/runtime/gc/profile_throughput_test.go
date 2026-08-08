package gc

import (
	"fmt"
	"strings"
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

func TestThroughputClassFreeMetadataIsReused(t *testing.T) {
	c := newTestCollector(t, Config{
		StressNurseryBytes:   64,
		ThroughputHeapBytes:  4096,
		ThroughputPageBytes:  4096,
		ThroughputClassLimit: 4096,
	})
	root := Root(Null())
	cls := -1
	for i := 0; i < 1024; i++ {
		obj, err := c.NewStructDefaultWithRoots(0, Slots{&root})
		if err != nil {
			t.Fatal(err)
		}
		root = Root(obj)
		if err := c.CollectMinor(Slots{&root}); err != nil {
			t.Fatal(err)
		}
		cls = int(c.entry(obj).class)
		root = Root(Null())
		if err := c.CollectFull(Slots{&root}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(c.throughput.freeSlots[cls]); got != 1 {
		t.Fatalf("throughput free-record metadata grew to %d entries, want 1 reusable entry", got)
	}
	if err := c.Verify(nil); err != nil {
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

var benchmarkThroughputLargeSpan int

func newThroughputLargeSpanMissFixture(spans int) throughputHeap {
	h := throughputHeap{
		largeFree:   make([]throughputLargeFree, spans),
		largestFree: 32,
	}
	for i := range h.largeFree {
		h.largeFree[i] = throughputLargeFree{off: uint32(i * 128), size: 32}
	}
	return h
}

func newThroughputLargeSpanChurnFixture(spans int) throughputHeap {
	memBytes := uint32(spans*128 + 128)
	h := throughputHeap{
		mem:         makeAlignedBytes(memBytes, 16),
		limit:       memBytes,
		pageBytes:   4096,
		bump:        memBytes,
		largeFree:   make([]throughputLargeFree, spans+1),
		largestFree: 128,
	}
	for i := 0; i < spans; i++ {
		h.largeFree[i] = throughputLargeFree{off: uint32(i * 128), size: 32}
	}
	h.largeFree[spans] = throughputLargeFree{off: uint32(spans * 128), size: 128}
	return h
}

func churnThroughputLargeSpan(h *throughputHeap) error {
	entry, err := h.alloc(64, spaceLarge)
	if err != nil {
		return err
	}
	return h.free(entry)
}

//go:noinline
func benchmarkFindThroughputLargeSpan(h *throughputHeap, size uint32) int {
	return h.findLarge(size)
}

func TestThroughputLargestFreeTracksMutations(t *testing.T) {
	h := throughputHeap{
		mem:       makeAlignedBytes(256, 16),
		limit:     256,
		pageBytes: 4096,
	}
	h.insertLargeFree(throughputLargeFree{off: 0, size: 64})
	h.insertLargeFree(throughputLargeFree{off: 128, size: 128})
	if h.largestFree != 128 || h.findLarge(96) != 1 {
		t.Fatalf("initial largest span = %d at %d, want 128 at 1", h.largestFree, h.findLarge(96))
	}
	entry, err := h.alloc(96, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if entry.off != 128 || h.largestFree != 128 || !h.largestFreeDirty {
		t.Fatalf("after split: entry offset=%d largest=%d dirty=%t", entry.off, h.largestFree, h.largestFreeDirty)
	}
	if err := h.verify(nil); err != nil {
		t.Fatalf("verify conservative largest span: %v", err)
	}
	if h.findLarge(80) != -1 || h.largestFree != 64 || h.largestFreeDirty {
		t.Fatalf("after split: entry offset=%d largest=%d find(80)=%d", entry.off, h.largestFree, h.findLarge(80))
	}
	h.insertLargeFree(throughputLargeFree{off: 64, size: 64})
	if h.largestFree != 128 || len(h.largeFree) != 2 {
		t.Fatalf("after coalesce: largest=%d spans=%v", h.largestFree, h.largeFree)
	}
}

func TestThroughputLargestFreeTracksCompleteRemoval(t *testing.T) {
	h := throughputHeap{
		mem:       makeAlignedBytes(256, 16),
		limit:     256,
		pageBytes: 4096,
		bump:      256,
	}
	h.insertLargeFree(throughputLargeFree{off: 0, size: 64})
	h.insertLargeFree(throughputLargeFree{off: 128, size: 32})
	entry, err := h.alloc(64, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if entry.off != 0 || len(h.largeFree) != 1 || h.largestFree != 64 || !h.largestFreeDirty {
		t.Fatalf("after removal: entry=%+v spans=%v largest=%d dirty=%t", entry, h.largeFree, h.largestFree, h.largestFreeDirty)
	}
	if err := h.verify(nil); err != nil {
		t.Fatalf("verify conservative bound after removal: %v", err)
	}
	if h.findLarge(48) != -1 || h.largestFree != 32 || h.largestFreeDirty {
		t.Fatalf("after removal refresh: largest=%d dirty=%t spans=%v", h.largestFree, h.largestFreeDirty, h.largeFree)
	}
}

func TestThroughputLargestFreeTracksDuplicateMaxima(t *testing.T) {
	h := throughputHeap{
		mem:       makeAlignedBytes(256, 16),
		limit:     256,
		pageBytes: 4096,
		bump:      256,
	}
	h.insertLargeFree(throughputLargeFree{off: 0, size: 64})
	h.insertLargeFree(throughputLargeFree{off: 128, size: 64})
	first, err := h.alloc(64, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if first.off != 0 || h.largestFree != 64 || !h.largestFreeDirty || h.findLarge(64) != 0 {
		t.Fatalf("after first maximum: entry=%+v largest=%d dirty=%t spans=%v", first, h.largestFree, h.largestFreeDirty, h.largeFree)
	}
	if err := h.verify(nil); err != nil {
		t.Fatalf("verify duplicate maximum bound: %v", err)
	}
	second, err := h.alloc(64, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if second.off != 128 || h.largestFree != 64 || !h.largestFreeDirty || len(h.largeFree) != 0 {
		t.Fatalf("after second maximum: entry=%+v largest=%d dirty=%t spans=%v", second, h.largestFree, h.largestFreeDirty, h.largeFree)
	}
	if h.findLarge(1) != -1 || h.largestFree != 0 || h.largestFreeDirty {
		t.Fatalf("after duplicate refresh: largest=%d dirty=%t spans=%v", h.largestFree, h.largestFreeDirty, h.largeFree)
	}
}

func TestThroughputVerifyRejectsStaleLargestFree(t *testing.T) {
	c := newTestCollector(t, Config{})
	c.throughput.largestFree = 32
	err := c.Verify(nil)
	if err == nil || !strings.Contains(err.Error(), "largest free span is stale") {
		t.Fatalf("Verify stale largest span error = %v", err)
	}
	c.throughput.largestFreeDirty = true
	c.throughput.largestFree = ^uint32(0)
	err = c.Verify(nil)
	if err == nil || !strings.Contains(err.Error(), "largest free span is stale") {
		t.Fatalf("Verify impossible dirty largest span error = %v", err)
	}
}

func TestThroughputLargestFreeFootprint(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("64-bit fixed-footprint assertion")
	}
	if got := unsafe.Sizeof(throughputHeap{}); got != 144 {
		t.Fatalf("throughputHeap size = %d, want 144", got)
	}
}

func BenchmarkThroughputLargeSpanMiss(b *testing.B) {
	for _, spans := range []int{64, 1024, 16384} {
		b.Run(fmt.Sprintf("spans=%d", spans), func(b *testing.B) {
			h := newThroughputLargeSpanMissFixture(spans)
			b.Run("warm", func(b *testing.B) {
				var found int
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					found += benchmarkFindThroughputLargeSpan(&h, 64)
				}
				b.StopTimer()
				benchmarkThroughputLargeSpan = found
				if found != -b.N || h.largestFree != 32 || h.largestFreeDirty {
					b.Fatalf("warm miss result=%d largest=%d dirty=%t", found, h.largestFree, h.largestFreeDirty)
				}
			})
			b.Run("cold", func(b *testing.B) {
				const batchSize = 64
				var batch [batchSize]throughputHeap
				var found, lastCount int
				b.ReportAllocs()
				for done := 0; done < b.N; {
					count := min(batchSize, b.N-done)
					lastCount = count
					b.StopTimer()
					for i := 0; i < count; i++ {
						batch[i] = h
						batch[i].largestFree = 64
						batch[i].largestFreeDirty = true
					}
					b.StartTimer()
					for i := 0; i < count; i++ {
						found += benchmarkFindThroughputLargeSpan(&batch[i], 64)
					}
					done += count
				}
				b.StopTimer()
				benchmarkThroughputLargeSpan = found
				if found != -b.N {
					b.Fatalf("cold miss result=%d, want %d", found, -b.N)
				}
				for i := 0; i < lastCount; i++ {
					if batch[i].largestFree != 32 || batch[i].largestFreeDirty {
						b.Fatalf("cold miss batch[%d] largest=%d dirty=%t", i, batch[i].largestFree, batch[i].largestFreeDirty)
					}
				}
			})
		})
	}
}

func BenchmarkThroughputLargeSpanChurn(b *testing.B) {
	for _, spans := range []int{64, 1024, 16384} {
		b.Run(fmt.Sprintf("spans=%d", spans), func(b *testing.B) {
			h := newThroughputLargeSpanChurnFixture(spans)
			// The first insertion grows the free-span scratch capacity. Keep
			// that one-time fixture cost outside the measured steady-state loop.
			if err := churnThroughputLargeSpan(&h); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := churnThroughputLargeSpan(&h); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if len(h.largeFree) != spans+1 || h.largeFree[spans].size != 128 || h.largestFree != 128 || h.largestFreeDirty {
				b.Fatalf("final churn state: spans=%d tail=%v largest=%d dirty=%t", len(h.largeFree), h.largeFree[len(h.largeFree)-1], h.largestFree, h.largestFreeDirty)
			}
			for i, span := range h.largeFree {
				wantOff, wantSize := uint32(i*128), uint32(32)
				if i == spans {
					wantSize = 128
				}
				if span.off != wantOff || span.size != wantSize {
					b.Fatalf("final churn span[%d]=%v, want off=%d size=%d", i, span, wantOff, wantSize)
				}
			}
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
