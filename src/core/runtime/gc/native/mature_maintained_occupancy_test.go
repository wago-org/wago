package gc

import (
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
