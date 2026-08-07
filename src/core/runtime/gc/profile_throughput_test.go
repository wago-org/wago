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
