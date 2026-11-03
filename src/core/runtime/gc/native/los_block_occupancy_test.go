package gc

import (
	"slices"
	"testing"
)

// losOccupancyOracle is deliberately independent of claimOccupancy,
// mutateOccupancy, and occupancyBlockLineRange. It derives expected metadata
// directly from physical extents and the old-only line invariant.
func losOccupancyOracle(t *testing.T, h *throughputHeap, entries []handleEntry) []matureOccupancyBlockState {
	t.Helper()
	want := make([]matureOccupancyBlockState, occupancyBlockCount(h.bump))
	for _, e := range entries {
		n := h.handleAllocSize(e)
		end := uint64(e.off) + uint64(n)
		if n == 0 || end > uint64(h.bump) {
			t.Fatalf("oracle invalid extent: %+v physical=%d bump=%d", e, n, h.bump)
		}
		first := e.off / matureOccupancyBlock
		last := uint32((end - 1) / uint64(matureOccupancyBlock))
		for b := first; b <= last; b++ {
			state := &want[b]
			if e.space == spaceOld {
				blockLo := uint64(b) * uint64(matureOccupancyBlock)
				lo := max(uint64(e.off), blockLo)
				hi := min(end, blockLo+uint64(matureOccupancyBlock))
				lineFirst := (lo - blockLo) / uint64(matureOccupancyLine)
				lineLast := (hi - 1 - blockLo) / uint64(matureOccupancyLine)
				for line := lineFirst; line <= lineLast; line++ {
					state.lineRefs[line]++
				}
			}
			if e.space == spaceLarge || n > matureOccupancyBlock {
				state.blockers++
			}
			if e.space == spaceOld && b < last {
				state.crossNext = true
			}
		}
		if e.space == spaceOld {
			granule := (e.off % matureOccupancyBlock) / 16
			want[first].oldStarts[granule/64] |= uint64(1) << (granule % 64)
		}
	}
	return want
}

func assertLOSOccupancyOracle(t *testing.T, h *throughputHeap, live []handleEntry) {
	t.Helper()
	want := losOccupancyOracle(t, h, live)
	if !slices.Equal(h.mature.occupancy, want) {
		t.Fatalf("maintained metadata differs from independent oracle\n got=%+v\nwant=%+v", h.mature.occupancy, want)
	}
}

func newLOSOccupancyHeap(t *testing.T, bytes uint32) *throughputHeap {
	t.Helper()
	h := newMatureRunHeap(t)
	h.limit = bytes
	h.mem = makeAlignedBytes(bytes, 16)
	return &h
}

func TestLOSBlockOccupancyOrdersAndBoundaries(t *testing.T) {
	for _, releaseLOSFirst := range []bool{false, true} {
		name := "old-first"
		if releaseLOSFirst {
			name = "los-first"
		}
		t.Run(name, func(t *testing.T) {
			h := newLOSOccupancyHeap(t, 4*matureOccupancyBlock)
			oldA, err := h.alloc(32, spaceOld)
			if err != nil {
				t.Fatal(err)
			}
			losA, err := h.alloc(32, spaceLarge)
			if err != nil {
				t.Fatal(err)
			}
			losB, err := h.alloc(32, spaceLarge)
			if err != nil {
				t.Fatal(err)
			}
			oldLarge, err := h.alloc(matureOccupancyBlock+16, spaceOld)
			if err != nil {
				t.Fatal(err)
			}
			live := []handleEntry{oldA, losA, losB, oldLarge}
			assertLOSOccupancyOracle(t, h, live)
			if h.mature.occupancy[0].lineRefs[0] != 2 || h.mature.occupancy[0].blockers != 3 {
				t.Fatalf("same-line owners or blockers lost: %+v", h.mature.occupancy[0])
			}
			first, second := oldA, losA
			if releaseLOSFirst {
				first, second = losA, oldA
			}
			for _, e := range []handleEntry{first, second, losB, oldLarge} {
				if err = h.free(e); err != nil {
					t.Fatal(err)
				}
				live = slices.DeleteFunc(live, func(l handleEntry) bool { return l == e })
				assertLOSOccupancyOracle(t, h, live)
			}
		})
	}
}

func TestLOSBlockOccupancyAbsorbedTailsAndAtomicFailure(t *testing.T) {
	h := newLOSOccupancyHeap(t, 4*matureOccupancyBlock)
	los, err := h.alloc(48, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.free(los); err != nil {
		t.Fatal(err)
	}
	reusedLOS, err := h.alloc(32, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	if reusedLOS.class&handleAbsorbedTailBit == 0 || h.handleAllocSize(reusedLOS) != 48 {
		t.Fatalf("LOS absorbed tail lost: %+v physical=%d", reusedLOS, h.handleAllocSize(reusedLOS))
	}
	oldSpan, err := h.alloc(48, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	oldGuard, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.free(oldSpan); err != nil {
		t.Fatal(err)
	}
	reusedOld, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if reusedOld.class&handleAbsorbedTailBit == 0 || h.handleAllocSize(reusedOld) != 48 {
		t.Fatalf("old absorbed tail lost: %+v physical=%d", reusedOld, h.handleAllocSize(reusedOld))
	}
	assertLOSOccupancyOracle(t, h, []handleEntry{guard, reusedLOS, reusedOld, oldGuard})

	wide, err := h.alloc(2*matureOccupancyBlock+16, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	before := slices.Clone(h.mature.occupancy)
	last := uint32((uint64(wide.off) + uint64(h.handleAllocSize(wide)) - 1) / uint64(matureOccupancyBlock))
	h.mature.occupancy[last].blockers = 0
	corrupt := slices.Clone(h.mature.occupancy)
	if err = h.releaseOccupancy(wide); err == nil {
		t.Fatal("release accepted late-block blocker corruption")
	}
	if !slices.Equal(h.mature.occupancy, corrupt) {
		t.Fatal("failed validation partially mutated earlier blocks")
	}
	h.mature.occupancy = before
	for _, e := range []handleEntry{wide, guard, reusedLOS, reusedOld, oldGuard} {
		if err = h.free(e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLOSBlockOccupancySelectorAfterLastRelease(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true, ThroughputHeapBytes: 4 * matureOccupancyBlock})
	old, err := c.throughput.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	losA, err := c.throughput.alloc(32, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	losB, err := c.throughput.alloc(32, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	c.handles = []handleEntry{{}, old, losA, losB}
	assertLOSOccupancyOracle(t, &c.throughput, c.handles[1:])
	for i, los := range []handleEntry{losA, losB} {
		c.throughput.mature.pressure = true
		s, selectErr := c.selectMatureEvacuation()
		if selectErr != nil || len(s.moves) != 0 {
			t.Fatalf("LOS block selected before release %d: moves=%v err=%v", i, s.moves, selectErr)
		}
		if err = c.throughput.free(los); err != nil {
			t.Fatal(err)
		}
		c.handles = slices.DeleteFunc(c.handles, func(e handleEntry) bool { return e == los })
		assertLOSOccupancyOracle(t, &c.throughput, c.handles[1:])
	}
	c.throughput.mature.pressure = true
	s, err := c.selectMatureEvacuation()
	if err != nil || len(s.moves) != 1 || s.moves[0].handle != 1 {
		t.Fatalf("old-only block not eligible after last LOS release: moves=%v err=%v", s.moves, err)
	}
}

func TestLOSBlockOccupancyOperationCensus(t *testing.T) {
	const size, objects = uint64(65552), uint64(256)
	lines := (size + uint64(matureOccupancyLine) - 1) / uint64(matureOccupancyLine)
	blocks := (size + uint64(matureOccupancyBlock) - 1) / uint64(matureOccupancyBlock)
	if lines != 257 || blocks != 3 {
		t.Fatalf("geometry lines=%d blocks=%d", lines, blocks)
	}
	if got := objects * blocks * 4; got != 3072 {
		t.Fatalf("LOS block visits=%d", got)
	}
	if old := objects * lines * 4; old != 263168 {
		t.Fatalf("former LOS line visits=%d", old)
	}
}

func TestLOSBlockOccupancyFinalBlockBounds(t *testing.T) {
	first, last := occupancyBlockLineRange(0xfffffff0, 0x100000000, 131071)
	if first != 127 || last != 127 {
		t.Fatalf("final block range=%d..%d", first, last)
	}
}
