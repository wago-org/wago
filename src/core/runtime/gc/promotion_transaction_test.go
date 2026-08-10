package gc

import (
	"errors"
	"slices"
	"strconv"
	"testing"
)

type promotionStateSnapshot struct {
	nursery        []byte
	nurseryBump    uint32
	survivorBump   uint32
	survivorFrom   uint8
	threshold      uint8
	handles        []handleEntry
	nurseryHandles []uint32
	mark           []bool
	remembered     []uint32
	objectCards    []objectCard
	freeCardSlot   uint32
	slotCards      []slotCard
	globalCardBits []uint64
	tableCardBits  []uint64
	cardFallback   bool
	throughput     throughputHeap
}

func snapshotPromotionState(c *Collector) promotionStateSnapshot {
	h := cloneThroughputHeap(c.throughput)
	mark := make([]bool, len(c.handles))
	copy(mark, c.mark)
	return promotionStateSnapshot{
		nursery:        slices.Clone(c.nursery),
		nurseryBump:    c.nurseryBump,
		survivorBump:   c.survivorBump,
		survivorFrom:   c.survivorFrom,
		threshold:      c.tenuringThreshold,
		handles:        slices.Clone(c.handles),
		nurseryHandles: slices.Clone(c.nurseryHandles),
		mark:           mark,
		remembered:     slices.Clone(c.remembered),
		objectCards:    slices.Clone(c.objectCards),
		freeCardSlot:   c.freeObjectCardSlot,
		slotCards:      slices.Clone(c.slotCards),
		globalCardBits: slices.Clone(c.globalCardBits),
		tableCardBits:  slices.Clone(c.tableCardBits),
		cardFallback:   c.cardFallback,
		throughput:     h,
	}
}

func cloneThroughputHeap(h throughputHeap) throughputHeap {
	h.mem = slices.Clone(h.mem)
	h.spanNodes = slices.Clone(h.spanNodes)
	h.pendingFree = slices.Clone(h.pendingFree)
	return h
}

func throughputHeapsEquivalent(a, b throughputHeap) bool {
	return a.limit == b.limit && a.pageBytes == b.pageBytes && a.classLimit == b.classLimit && a.bump == b.bump &&
		a.freeBytes == b.freeBytes && a.pendingBytes == b.pendingBytes && a.spanCount == b.spanCount && len(a.spanNodes) == len(b.spanNodes) &&
		slices.Equal(a.mem, b.mem) && slices.Equal(a.freeSpans(), b.freeSpans()) && slices.Equal(a.pendingFree, b.pendingFree)
}

func TestThroughputAllocationRollbackRestoresAllPaths(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *throughputHeap) uint32
		space spaceKind
	}{
		{
			name: "bump_growth",
			setup: func(t *testing.T, h *throughputHeap) uint32 {
				return 32
			},
			space: spaceOld,
		},
		{
			name: "size_class_free_span",
			setup: func(t *testing.T, h *throughputHeap) uint32 {
				e, err := h.alloc(32, spaceOld)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.alloc(32, spaceOld); err != nil {
					t.Fatal(err)
				}
				if err := h.free(e); err != nil {
					t.Fatalf("prepare class free span: %v", err)
				}
				return 32
			},
			space: spaceOld,
		},
		{
			name: "large_span_split",
			setup: func(t *testing.T, h *throughputHeap) uint32 {
				e, err := h.alloc(160, spaceLarge)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.alloc(32, spaceLarge); err != nil {
					t.Fatal(err)
				}
				if err := h.free(e); err != nil {
					t.Fatalf("prepare large free span: %v", err)
				}
				return 64
			},
			space: spaceLarge,
		},
		{
			name: "large_span_remove",
			setup: func(t *testing.T, h *throughputHeap) uint32 {
				e, err := h.alloc(64, spaceLarge)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.alloc(32, spaceLarge); err != nil {
					t.Fatal(err)
				}
				if err := h.free(e); err != nil {
					t.Fatalf("prepare large free span: %v", err)
				}
				return 64
			},
			space: spaceLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var h throughputHeap
			if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 32}); err != nil {
				t.Fatal(err)
			}
			size := test.setup(t, &h)
			before := cloneThroughputHeap(h)
			transaction := h.beginAllocTransaction()
			e, err := h.alloc(size, test.space)
			if err != nil {
				t.Fatal(err)
			}
			h.rollbackSuccessfulAlloc(e, transaction.bump)
			h.restoreAllocTransaction(transaction)
			if !throughputHeapsEquivalent(h, before) {
				t.Fatalf("allocator differs after restoring %s allocation", test.name)
			}
		})
	}
}

func TestThroughputAllocationTransactionRollsBackMixedSequence(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 64 << 10, ThroughputPageBytes: 4096, ThroughputClassLimit: 128}); err != nil {
		t.Fatal(err)
	}
	classFree, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.alloc(64, spaceOld); err != nil {
		t.Fatal(err)
	}
	largeFree, err := h.alloc(160, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.free(classFree); err != nil {
		t.Fatal(err)
	}
	if err := h.free(largeFree); err != nil {
		t.Fatal(err)
	}
	before := cloneThroughputHeap(h)
	tx := h.beginAllocTransaction()
	requests := []struct {
		size  uint32
		space spaceKind
	}{
		{32, spaceOld},    // reusable class slot
		{64, spaceOld},    // bump allocation
		{64, spaceLarge},  // split the reusable large span
		{96, spaceLarge},  // consume its remainder
		{256, spaceLarge}, // bump allocation
	}
	allocated := make([]handleEntry, 0, len(requests))
	for _, request := range requests {
		e, err := h.alloc(request.size, request.space)
		if err != nil {
			t.Fatal(err)
		}
		allocated = append(allocated, e)
	}
	for i := len(allocated) - 1; i >= 0; i-- {
		h.rollbackSuccessfulAlloc(allocated[i], tx.bump)
	}
	h.restoreAllocTransaction(tx)
	if !throughputHeapsEquivalent(h, before) {
		t.Fatal("mixed allocation sequence did not restore equivalent allocator state")
	}
}

func assertPromotionStateEqual(t *testing.T, c *Collector, want promotionStateSnapshot) {
	t.Helper()
	got := snapshotPromotionState(c)
	if !slices.Equal(got.nursery, want.nursery) {
		t.Fatal("failed promotion mutated nursery bytes")
	}
	if got.nurseryBump != want.nurseryBump || got.survivorBump != want.survivorBump || got.survivorFrom != want.survivorFrom || got.threshold != want.threshold || !slices.Equal(got.nurseryHandles, want.nurseryHandles) {
		t.Fatal("failed promotion mutated nursery allocation metadata")
	}
	if !slices.Equal(got.handles, want.handles) {
		t.Fatalf("failed promotion mutated handles:\n got %#v\nwant %#v", got.handles, want.handles)
	}
	if !slices.Equal(got.mark, want.mark) {
		t.Fatalf("failed promotion mutated marks: got %v want %v", got.mark, want.mark)
	}
	if !slices.Equal(got.remembered, want.remembered) || !slices.Equal(got.objectCards, want.objectCards) || got.freeCardSlot != want.freeCardSlot ||
		!slices.Equal(got.slotCards, want.slotCards) ||
		!slices.Equal(got.globalCardBits, want.globalCardBits) || !slices.Equal(got.tableCardBits, want.tableCardBits) || got.cardFallback != want.cardFallback {
		t.Fatal("failed promotion mutated remembered or card metadata")
	}
	if !throughputHeapsEquivalent(got.throughput, want.throughput) {
		t.Fatal("failed promotion mutated throughput allocator intervals, metadata capacity, or backing")
	}
}

func TestPromotionPlanningFailureRestoresExactCollectorState(t *testing.T) {
	for destinationsBeforeFailure := 0; destinationsBeforeFailure < 4; destinationsBeforeFailure++ {
		t.Run(strconv.Itoa(destinationsBeforeFailure), func(t *testing.T) {
			c := newTestCollector(t, Config{
				NurseryBytes:        4096,
				ThroughputHeapBytes: 4096,
				ThroughputPageBytes: 4096,
			})
			roots := make([]Root, 4)
			for i := range roots {
				r, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				roots[i] = Root(r)
			}
			objectBytes := c.entry(Ref(roots[0])).allocSize
			c.throughput.mem = makeAlignedBytes(c.throughput.limit, 16)
			c.throughput.bump = c.throughput.limit - uint32(destinationsBeforeFailure)*objectBytes
			before := snapshotPromotionState(c)

			err := c.CollectMinor(stressRootSlots(roots))
			if !errors.Is(err, errThroughputHeapExhausted) {
				t.Fatalf("CollectMinor error = %v, want throughput exhaustion", err)
			}
			assertPromotionStateEqual(t, c, before)
		})
	}
}
