package gc

import (
	"slices"
	"strings"
	"testing"
	"unsafe"
)

// seedMaintainedOccupancy supports selector fixtures that intentionally build
// exact allocator/handle state without using the production allocation path.
func seedMaintainedOccupancy(t *testing.T, c *Collector) {
	t.Helper()
	h := &c.throughput
	h.mature.occupancy = nil
	h.ensureOccupancy(h.bump)
	for i, e := range c.handles {
		if i == 0 || (e.space != spaceOld && e.space != spaceLarge) {
			continue
		}
		if err := h.claimOccupancy(e); err != nil {
			t.Fatalf("seed maintained occupancy for handle %d: %v", i, err)
		}
	}
}

func TestMaintainedOccupancyIndexedGrowthRollback(t *testing.T) {
	h := newMatureRunHeap(t)
	h.limit = 65536
	h.mem = makeAlignedBytes(65536, 16)
	h.bump = 32768
	h.ensureOccupancy(h.bump)
	guard := handleEntry{off: 32736, size: 32, space: spaceOld}
	if err := h.claimOccupancy(guard); err != nil {
		t.Fatal(err)
	}
	if err := h.insertFreeSpan(throughputFreeSpan{off: 0, size: 32736}); err != nil {
		t.Fatal(err)
	}
	h.mature.occupancy = slices.Clip(h.mature.occupancy)
	before := slices.Clone(h.mature.occupancy)
	h.reserveOccupancyCapacity(uint64(h.bump) + 32 + 32768)
	tx := h.beginAllocTransaction()
	a, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.alloc(32768, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if a.off != 0 || b.off != 32768 {
		t.Fatalf("unexpected destinations %+v %+v", a, b)
	}
	h.rollbackSuccessfulAlloc(b, tx.bump)
	h.rollbackSuccessfulAlloc(a, tx.bump)
	h.restoreAllocTransaction(tx)
	if !slices.Equal(before, h.mature.occupancy) {
		t.Fatalf("rollback resurrected indexed claim: refs=%d starts=%x", h.mature.occupancy[0].lineRefs[0], h.mature.occupancy[0].oldStarts[0])
	}
}

func TestOccupancyBlockLineRangePortableBounds(t *testing.T) {
	tests := []struct {
		name        string
		off, end    uint64
		block       uint32
		first, last uint32
	}{
		{"final-block-first-line", 0xffff8000, 0xffff8010, 131071, 0, 0},
		{"final-block-partial-end", 0xfffffff0, 0x100000000, 131071, 127, 127},
		{"full-block", 32768, 65536, 1, 0, 127},
		{"cross-line-left", 32752, 32784, 0, 127, 127},
		{"cross-line-right", 32752, 32784, 1, 0, 0},
		{"cross-line", 240, 272, 0, 0, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			first, last := occupancyBlockLineRange(tc.off, tc.end, tc.block)
			if first != tc.first || last != tc.last {
				t.Fatalf("range = %d..%d, want %d..%d", first, last, tc.first, tc.last)
			}
		})
	}
}

func TestMaintainedOccupancySharedLineAndExactIdentity(t *testing.T) {
	h := newMatureRunHeap(t)
	a, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	state := h.mature.occupancy[0]
	if state.lineRefs[0] != 2 || state.oldStarts[0]&5 != 5 || state.blockers != 0 || state.crossNext {
		t.Fatalf("initial occupancy = %+v", state)
	}
	if err = h.free(a); err != nil {
		t.Fatal(err)
	}
	state = h.mature.occupancy[0]
	if state.lineRefs[0] != 1 || state.oldStarts[0]&5 != 4 {
		t.Fatalf("shared-line release = %+v", state)
	}
	if err = h.free(b); err != nil {
		t.Fatal(err)
	}
	if h.bump != 0 || len(h.mature.occupancy) != 0 {
		t.Fatalf("final release retained committed metadata: bump=%d blocks=%d", h.bump, len(h.mature.occupancy))
	}
}

func TestMaintainedOccupancyCrossBlockAndLOSBlockers(t *testing.T) {
	h := newMatureRunHeap(t)
	h.mem = make([]byte, 3*matureOccupancyBlock)
	h.limit = uint32(len(h.mem))
	h.bump = matureOccupancyBlock - 16
	cross, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	los, err := h.alloc(16, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if !h.mature.occupancy[0].crossNext || h.mature.occupancy[0].oldStarts[31]&(uint64(1)<<63) == 0 ||
		h.mature.occupancy[1].blockers != 1 || h.mature.occupancy[1].oldStarts[0] != 0 {
		t.Fatalf("cross/LOS occupancy = %+v %+v", h.mature.occupancy[0], h.mature.occupancy[1])
	}
	if err = h.free(los); err != nil {
		t.Fatal(err)
	}
	if err = h.free(cross); err != nil {
		t.Fatal(err)
	}
}

func TestMaintainedOccupancyPolicyAndVerification(t *testing.T) {
	var ordinary throughputHeap
	cfg, err := normalizeConfig(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ordinary.Init(cfg); err != nil {
		t.Fatal(err)
	}
	if ordinary.mature != nil {
		t.Fatal("default throughput policy allocated mature metadata")
	}
	wantSize := uintptr(392)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		wantSize = 388
	}
	if unsafe.Sizeof(matureOccupancyBlockState{}) != wantSize {
		t.Fatalf("occupancy block size = %d", unsafe.Sizeof(matureOccupancyBlockState{}))
	}

	h := newMatureRunHeap(t)
	e, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	handles := []handleEntry{{}, e}
	if err = h.verify(handles); err != nil {
		t.Fatal(err)
	}
	h.mature.occupancy[0].oldStarts[0] &^= 1
	if err = h.verify(handles); err == nil || !strings.Contains(err.Error(), "occupancy mismatch") {
		t.Fatalf("missing start verification error = %v", err)
	}
}
