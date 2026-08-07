package gc

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

type promotionStateSnapshot struct {
	nursery        []byte
	nurseryBump    uint32
	handles        []handleEntry
	nurseryHandles []uint32
	mark           []bool
	remembered     []uint32
	objectCards    []objectCard
	slotCards      []slotCard
	slotCardSlot   map[uint64]uint32
	throughput     throughputHeap
}

func snapshotPromotionState(c *Collector) promotionStateSnapshot {
	h := cloneThroughputHeap(c.throughput)
	mark := make([]bool, len(c.handles))
	copy(mark, c.mark)
	return promotionStateSnapshot{
		nursery:        slices.Clone(c.nursery),
		nurseryBump:    c.nurseryBump,
		handles:        slices.Clone(c.handles),
		nurseryHandles: slices.Clone(c.nurseryHandles),
		mark:           mark,
		remembered:     slices.Clone(c.remembered),
		objectCards:    slices.Clone(c.objectCards),
		slotCards:      slices.Clone(c.slotCards),
		slotCardSlot:   maps.Clone(c.slotCardSlot),
		throughput:     h,
	}
}

func cloneThroughputHeap(h throughputHeap) throughputHeap {
	h.mem = slices.Clone(h.mem)
	h.freeHeads = slices.Clone(h.freeHeads)
	h.freeRecordHeads = slices.Clone(h.freeRecordHeads)
	h.largeFree = slices.Clone(h.largeFree)
	h.freeSlots = slices.Clone(h.freeSlots)
	for i := range h.freeSlots {
		h.freeSlots[i] = slices.Clone(h.freeSlots[i])
	}
	return h
}

func TestThroughputAllocationCheckpointRestoresAllPaths(t *testing.T) {
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
			name: "size_class_free_list",
			setup: func(t *testing.T, h *throughputHeap) uint32 {
				e, err := h.alloc(32, spaceOld)
				if err != nil || h.free(e) != nil {
					t.Fatalf("prepare class free list: %v", err)
				}
				return 32
			},
			space: spaceOld,
		},
		{
			name: "large_span_split",
			setup: func(t *testing.T, h *throughputHeap) uint32 {
				e, err := h.alloc(160, spaceLarge)
				if err != nil || h.free(e) != nil {
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
				if err != nil || h.free(e) != nil {
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
			checkpoint := h.checkpointAlloc(size, test.space)
			if _, err := h.alloc(size, test.space); err != nil {
				t.Fatal(err)
			}
			h.restoreAlloc(checkpoint)
			h.restoreAllocTransaction(transaction)
			if !reflect.DeepEqual(h, before) {
				t.Fatalf("allocator differs after restoring %s checkpoint", test.name)
			}
		})
	}
}

func assertPromotionStateEqual(t *testing.T, c *Collector, want promotionStateSnapshot) {
	t.Helper()
	got := snapshotPromotionState(c)
	if !slices.Equal(got.nursery, want.nursery) {
		t.Fatal("failed promotion mutated nursery bytes")
	}
	if got.nurseryBump != want.nurseryBump || !slices.Equal(got.nurseryHandles, want.nurseryHandles) {
		t.Fatal("failed promotion mutated nursery allocation metadata")
	}
	if !slices.Equal(got.handles, want.handles) {
		t.Fatalf("failed promotion mutated handles:\n got %#v\nwant %#v", got.handles, want.handles)
	}
	if !slices.Equal(got.mark, want.mark) {
		t.Fatalf("failed promotion mutated marks: got %v want %v", got.mark, want.mark)
	}
	if !slices.Equal(got.remembered, want.remembered) || !slices.Equal(got.objectCards, want.objectCards) ||
		!slices.Equal(got.slotCards, want.slotCards) || !maps.Equal(got.slotCardSlot, want.slotCardSlot) {
		t.Fatal("failed promotion mutated remembered or card metadata")
	}
	if !reflect.DeepEqual(got.throughput, want.throughput) {
		t.Fatal("failed promotion mutated throughput allocator metadata or backing")
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
