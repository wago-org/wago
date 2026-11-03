package gc

import (
	"reflect"
	"slices"
	"testing"
	"unsafe"
)

type exactEvacuationState struct {
	mem, spanNodes, pendingFree, regions, touched unsafe.Pointer
	bump, spanRoot, freeNodeHead                  uint32
	freeBytes, pendingBytes                       uint64
	spanCount                                     uint32
	memLen, memCap                                int
	nodesLen, nodesCap                            int
	pendingLen, pendingCap                        int
	regionsLen, regionsCap                        int
	touchedLen, touchedCap                        int
	nodes                                         []throughputSpanNode
	pending                                       []throughputFreeSpan
	matureRegions                                 []matureRegion
	matureValue                                   matureHeap
}

func exactEvacuationSnapshot(h *throughputHeap) exactEvacuationState {
	matureValue := *h.mature
	matureValue.regions = slices.Clone(h.mature.regions)
	matureValue.touched = slices.Clone(h.mature.touched)
	return exactEvacuationState{
		mem: unsafe.Pointer(unsafe.SliceData(h.mem)), spanNodes: unsafe.Pointer(unsafe.SliceData(h.spanNodes)),
		pendingFree: unsafe.Pointer(unsafe.SliceData(h.pendingFree)), regions: unsafe.Pointer(unsafe.SliceData(h.mature.regions)),
		touched: unsafe.Pointer(unsafe.SliceData(h.mature.touched)), bump: h.bump, spanRoot: h.spanRoot,
		freeNodeHead: h.freeNodeHead, freeBytes: h.freeBytes, pendingBytes: h.pendingBytes, spanCount: h.spanCount,
		memLen: len(h.mem), memCap: cap(h.mem), nodesLen: len(h.spanNodes), nodesCap: cap(h.spanNodes),
		pendingLen: len(h.pendingFree), pendingCap: cap(h.pendingFree), regionsLen: len(h.mature.regions), regionsCap: cap(h.mature.regions),
		touchedLen: len(h.mature.touched), touchedCap: cap(h.mature.touched), nodes: slices.Clone(h.spanNodes),
		pending: slices.Clone(h.pendingFree), matureRegions: slices.Clone(h.mature.regions), matureValue: matureValue,
	}
}

func assertExactEvacuationState(t *testing.T, h *throughputHeap, want exactEvacuationState) {
	t.Helper()
	got := exactEvacuationSnapshot(h)
	if got.mem != want.mem || got.spanNodes != want.spanNodes || got.pendingFree != want.pendingFree || got.regions != want.regions || got.touched != want.touched ||
		got.bump != want.bump || got.spanRoot != want.spanRoot || got.freeNodeHead != want.freeNodeHead || got.freeBytes != want.freeBytes ||
		got.pendingBytes != want.pendingBytes || got.spanCount != want.spanCount || got.memLen != want.memLen || got.memCap != want.memCap ||
		got.nodesLen != want.nodesLen || got.nodesCap != want.nodesCap || got.pendingLen != want.pendingLen || got.pendingCap != want.pendingCap ||
		got.regionsLen != want.regionsLen || got.regionsCap != want.regionsCap || got.touchedLen != want.touchedLen || got.touchedCap != want.touchedCap {
		t.Fatalf("evacuation rollback changed storage headers: got mem=%p/%d/%d nodes=%p/%d/%d pending=%p/%d/%d regions=%p/%d/%d touched=%p/%d/%d scalars=%d/%d/%d/%d/%d/%d; want mem=%p/%d/%d nodes=%p/%d/%d pending=%p/%d/%d regions=%p/%d/%d touched=%p/%d/%d scalars=%d/%d/%d/%d/%d/%d",
			got.mem, got.memLen, got.memCap, got.spanNodes, got.nodesLen, got.nodesCap, got.pendingFree, got.pendingLen, got.pendingCap, got.regions, got.regionsLen, got.regionsCap, got.touched, got.touchedLen, got.touchedCap, got.bump, got.spanRoot, got.freeNodeHead, got.freeBytes, got.pendingBytes, got.spanCount,
			want.mem, want.memLen, want.memCap, want.spanNodes, want.nodesLen, want.nodesCap, want.pendingFree, want.pendingLen, want.pendingCap, want.regions, want.regionsLen, want.regionsCap, want.touched, want.touchedLen, want.touchedCap, want.bump, want.spanRoot, want.freeNodeHead, want.freeBytes, want.pendingBytes, want.spanCount)
	}
	if !slices.Equal(got.nodes, want.nodes) {
		t.Fatal("evacuation rollback changed AVL/index/node-free storage")
	}
	if !slices.Equal(got.pending, want.pending) {
		t.Fatal("evacuation rollback changed pending debt storage")
	}
	if !slices.Equal(got.matureRegions, want.matureRegions) {
		for i := range got.matureRegions {
			if got.matureRegions[i] != want.matureRegions[i] {
				t.Fatalf("evacuation rollback changed mature region %d: got %#v want %#v", i, got.matureRegions[i], want.matureRegions[i])
			}
		}
		t.Fatal("evacuation rollback changed mature occupancy/reservations")
	}
	if !reflect.DeepEqual(got.matureValue, want.matureValue) {
		t.Fatalf("evacuation rollback changed mature state: got %#v want %#v", got.matureValue, want.matureValue)
	}
}

func newFragmentedEvacuationHeap(t *testing.T, pairs int) throughputHeap {
	t.Helper()
	var h throughputHeap
	if err := h.Init(Config{NurseryRegionShift: 16, ThroughputHeapBytes: 8 << 20, ThroughputPageBytes: 4096, ThroughputClassLimit: 32}); err != nil {
		t.Fatal(err)
	}
	holes := make([]handleEntry, pairs)
	for i := range holes {
		var err error
		if holes[i], err = h.alloc(32, spaceOld); err != nil {
			t.Fatal(err)
		}
		if _, err = h.alloc(32, spaceOld); err != nil {
			t.Fatal(err)
		}
	}
	for _, hole := range holes {
		if err := h.free(hole); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func TestEvacuationTransactionRestoresExactAllocatorStorage(t *testing.T) {
	h := newFragmentedEvacuationHeap(t, 256)
	before := exactEvacuationSnapshot(&h)
	indexed := h.freeSpans()[127]
	bump := throughputFreeSpan{off: h.bump, size: 8192}
	tx := h.beginEvacuationTransaction()
	h.beginMatureAdmission(true)
	for _, run := range []throughputFreeSpan{indexed, bump} {
		if err := h.reserveEvacuationRun(run); err != nil {
			t.Fatal(err)
		}
	}
	claims := newRunClaimPlanner([]throughputFreeSpan{indexed, bump}, 2)
	if _, ok := claims.destination(0, indexed.size-16); !ok {
		t.Fatal("indexed destination")
	}
	if _, ok := claims.destination(1, bump.size-16); !ok {
		t.Fatal("bump destination")
	}
	if err := claims.releaseTails(&h); err != nil {
		t.Fatal(err)
	}
	if len(tx.spanNodes) == 0 || len(tx.regions) == 0 {
		t.Fatalf("transaction did not observe metadata writes: nodes=%d regions=%d", len(tx.spanNodes), len(tx.regions))
	}
	h.rollbackEvacuation(tx)
	assertExactEvacuationState(t, &h, before)
}

func TestEvacuationTransactionWorkTracksTouchedMetadata(t *testing.T) {
	h := newFragmentedEvacuationHeap(t, 4096)
	run := h.freeSpans()[2048]
	tx := h.beginEvacuationTransaction()
	h.beginMatureAdmission(false)
	if err := h.reserveEvacuationRun(run); err != nil {
		t.Fatal(err)
	}
	// This untimed census distinguishes a bounded mutation journal from copying
	// the allocator-wide node arena for one small admitted run.
	if got, whole := len(tx.spanNodes), len(h.spanNodes); got == 0 || got*16 >= whole {
		t.Fatalf("journal writes=%d allocator nodes=%d; work was not bounded by the touched path", got, whole)
	}
	h.rollbackEvacuation(tx)
}
