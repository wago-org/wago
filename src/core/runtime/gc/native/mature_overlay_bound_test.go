package gc

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"unsafe"
)

func TestMatureEvacuationOverlayUsesCommittedSpan(t *testing.T) {
	c := newTestCollector(t, Config{
		NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true,
		ThroughputHeapBytes: 1 << 30, ThroughputPageBytes: 4096,
	})
	r, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(r); err != nil {
		t.Fatal(err)
	}
	h := &c.throughput
	h.mature.pressure = true
	s, err := c.selectMatureEvacuation()
	if err != nil {
		t.Fatal(err)
	}
	if h.limit != 1<<30 || h.bump != 32 || len(h.mem) != 4096 {
		t.Fatalf("configured/active/backing = %d/%d/%d", h.limit, h.bump, len(h.mem))
	}
	if len(s.excluded) != 1 || len(s.moves) != 1 || s.moves[0].handle != handleOf(r) {
		t.Fatalf("active overlay/candidates = %d/%+v", len(s.excluded), s.moves)
	}

	// A destination in the uncommitted configured tail is outside the source
	// overlay and remains admissible; the partial final run gets exact credit.
	runs, credit, need, err := planEvacuationRuns(
		[]throughputFreeSpan{{off: uint32(evacuationBlock), size: uint32(evacuationBlock + 96)}},
		s.excluded, 32, 32, 1, 2,
	)
	if err != nil || len(runs) != 2 || credit != need || need != evacuationBlock+32 {
		t.Fatalf("tail destination = %+v credit=%d need=%d err=%v", runs, credit, need, err)
	}
}

func TestMatureEvacuationOverlayValidatesBeforeIndexing(t *testing.T) {
	c := newTestCollector(t, Config{
		NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true,
		ThroughputHeapBytes: 1 << 30, ThroughputPageBytes: 4096,
	})
	h := &c.throughput
	h.mature.pressure = true
	h.mature.cursor = 17
	c.handles = []handleEntry{{}, {
		off: uint32(evacuationBlock), size: 32, space: spaceOld,
		class: uint16(len(throughputClassSizes)),
	}}
	h.bump = 32
	h.mem = make([]byte, 4096)
	beforeHandles := slices.Clone(c.handles)
	beforeHeap := *h
	if _, err := c.selectMatureEvacuation(); err == nil || !strings.Contains(err.Error(), "source extent") {
		t.Fatalf("malformed physical extent error = %v", err)
	}
	if !reflect.DeepEqual(beforeHandles, c.handles) || h.bump != beforeHeap.bump || len(h.mem) != len(beforeHeap.mem) || h.mature.cursor != 17 {
		t.Fatal("malformed source changed ownership or cursor")
	}

	h.bump = 4097
	if _, err := c.selectMatureEvacuation(); err == nil || !strings.Contains(err.Error(), "heap span") {
		t.Fatalf("unbacked active span error = %v", err)
	}
}

func TestMatureEvacuationCursorUsesCurrentActiveModulus(t *testing.T) {
	c := newTestCollector(t, Config{
		NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true,
		ThroughputHeapBytes: 32 * uint32(evacuationBlock), ThroughputPageBytes: 4096,
	})
	h := &c.throughput
	h.bump = 2 * uint32(evacuationBlock)
	h.mem = make([]byte, h.bump)
	h.mature.pressure = true
	h.mature.cursor = 5
	c.handles = []handleEntry{{},
		{off: 0, size: 32, space: spaceOld, class: uint16(len(throughputClassSizes))},
		{off: uint32(evacuationBlock), size: 32, space: spaceOld, class: uint16(len(throughputClassSizes))},
	}
	s, err := c.selectMatureEvacuationBounded(1)
	if err != nil || len(s.moves) != 1 || s.moves[0].handle != 2 || h.mature.cursor != 0 {
		t.Fatalf("active-modulus selection = %+v cursor=%d err=%v", s.moves, h.mature.cursor, err)
	}

	// A zero active span has no indexable storage and deterministically resets
	// the logical seed without manufacturing a block or candidate.
	c.handles = []handleEntry{{}}
	h.bump = 0
	h.mem = nil
	h.mature.cursor = 29
	s, err = c.selectMatureEvacuationBounded(1)
	if err != nil || len(s.excluded) != 0 || len(s.moves) != 0 || h.mature.cursor != 0 {
		t.Fatalf("zero-span selection = %+v cursor=%d err=%v", s, h.mature.cursor, err)
	}
}

func TestMatureEvacuationCandidateAndRunLimitsAreIndependent(t *testing.T) {
	c := newTestCollector(t, Config{
		NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true,
		ThroughputHeapBytes: 4 * uint32(evacuationBlock), ThroughputPageBytes: 4096,
	})
	h := &c.throughput
	h.bump = 2 * uint32(evacuationBlock)
	h.mem = make([]byte, h.bump)
	h.mature.pressure = true
	h.mature.cursor = 0
	c.handles = []handleEntry{{},
		{off: 0, size: 32, space: spaceOld, class: uint16(len(throughputClassSizes))},
		{off: uint32(evacuationBlock), size: 32, space: spaceOld, class: uint16(len(throughputClassSizes))},
	}
	before := slices.Clone(c.handles)
	s, err := c.selectMatureEvacuationBounded(1)
	if err != nil || len(s.moves) != 1 || s.moves[0].handle != 1 || c.CollectionDiagnostics().Evacuation != "budget" {
		t.Fatalf("candidate refusal = %+v err=%v diagnostics=%+v", s, err, c.CollectionDiagnostics())
	}
	if !reflect.DeepEqual(before, c.handles) || h.bump != 2*uint32(evacuationBlock) {
		t.Fatal("candidate refusal mutated ownership")
	}

	free := []throughputFreeSpan{{off: 2 * uint32(evacuationBlock), size: uint32(evacuationBlock)}, {off: 3 * uint32(evacuationBlock), size: 96}}
	freeBefore := slices.Clone(free)
	if _, _, _, err = planEvacuationRuns(free, s.excluded, 32, 32, 1, 1); err == nil || !strings.Contains(err.Error(), "run metadata limit") {
		t.Fatalf("independent run refusal = %v", err)
	}
	if !slices.Equal(free, freeBefore) || !reflect.DeepEqual(before, c.handles) {
		t.Fatal("run refusal mutated inputs or ownership")
	}
	if runs, _, _, err := planEvacuationRuns(free, s.excluded, 32, 32, 1, 2); err != nil || len(runs) != 2 {
		t.Fatalf("candidate refusal constrained run metadata: %+v %v", runs, err)
	}
}

func TestMatureEvacuationScratchStorageCensus(t *testing.T) {
	// Selection storage is one overlay/count/bad/selected/excluded record per
	// active block plus at most evacuationCandidateLimit moves. Later phases separately
	// cap destination runs; plans and source debt are one record per live move.
	wantOverlay, wantUndo := uintptr(40), uintptr(40)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		wantOverlay, wantUndo = 28, 32
	}
	if unsafe.Sizeof(evacuationOverlay{}) != wantOverlay || unsafe.Sizeof(matureMove{}) != 36 ||
		unsafe.Sizeof(plannedPromotion{}) != 20 || unsafe.Sizeof(throughputFreeSpan{}) != 8 ||
		unsafe.Sizeof(throughputSpanNodeUndo{}) != wantUndo {
		t.Fatalf("scratch record sizes changed: overlay=%d move=%d plan=%d span=%d undo=%d",
			unsafe.Sizeof(evacuationOverlay{}), unsafe.Sizeof(matureMove{}), unsafe.Sizeof(plannedPromotion{}),
			unsafe.Sizeof(throughputFreeSpan{}), unsafe.Sizeof(throughputSpanNodeUndo{}))
	}
	if evacuationCandidateLimit != 65536 || evacuationRunLimit != 65536 {
		t.Fatalf("candidate/run policy limits changed: %d/%d", evacuationCandidateLimit, evacuationRunLimit)
	}
}
