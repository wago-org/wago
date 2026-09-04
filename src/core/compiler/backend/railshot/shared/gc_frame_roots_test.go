package shared

import (
	"slices"
	"testing"
	"unsafe"
)

func TestGCFrameRootPlanFootprint(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("64-bit fixed-footprint assertion")
	}
	if got := unsafe.Sizeof(GCFrameRootPlan{}); got != 216 {
		t.Fatalf("GCFrameRootPlan size=%d, want 216", got)
	}
	if got := unsafe.Sizeof(GCModuleFrameRootPlan{}); got != 40 {
		t.Fatalf("GCModuleFrameRootPlan size=%d, want 40", got)
	}
}

func TestGCFrameSafepointStream(t *testing.T) {
	var plan GCFrameRootPlan
	if !plan.AppendSafepoint([]uint32{8, 24}) || !plan.AppendSafepoint(nil) || plan.SafepointCount() != 2 {
		t.Fatalf("safepoint stream = %#v, count %d", plan.SafepointData, plan.SafepointCount())
	}
	var got [][]uint32
	if !plan.VisitSafepoints(func(_ int, offsets []uint32) bool {
		got = append(got, append([]uint32(nil), offsets...))
		return true
	}) || len(got) != 2 || len(got[0]) != 2 || got[0][0] != 8 || got[0][1] != 24 || len(got[1]) != 0 {
		t.Fatalf("visited safepoints = %#v", got)
	}
	plan.SafepointData[0] = ^uint32(0)
	if plan.VisitSafepoints(func(int, []uint32) bool { return true }) {
		t.Fatal("malformed safepoint stream was accepted")
	}
}

func TestGCFrameCallsiteStream(t *testing.T) {
	var plan GCFrameRootPlan
	if !plan.AppendCallsite(24, 0, []uint32{8, 16}) || !plan.AppendCallsite(40, 64, nil) || plan.CallsiteCount() != 2 {
		t.Fatalf("callsite stream = %#v, count %d", plan.CallsiteData, plan.CallsiteCount())
	}
	first, ok := plan.Callsite(0)
	if !ok || first.ReturnOffset() != 24 || first.StackAdjust() != 0 || len(first.Offsets()) != 2 {
		t.Fatalf("first callsite = %#v, %v", first, ok)
	}
	if !plan.ShiftCallsiteReturnOffsets(32, 8) {
		t.Fatal("valid callsite shift failed")
	}
	second, ok := plan.Callsite(1)
	if !ok || second.ReturnOffset() != 32 || second.StackAdjust() != 64 || len(second.Offsets()) != 0 {
		t.Fatalf("second callsite = %#v, %v", second, ok)
	}
	plan.CallsiteData[2] = ^uint32(0)
	if plan.VisitCallsites(func(int, GCFrameCallsite) bool { return true }) {
		t.Fatal("malformed callsite stream was accepted")
	}
}

func TestGCFrameRootPlanWideMaskWords(t *testing.T) {
	plan := &GCFrameRootPlan{
		LocalIndexes:       make([]uint32, 65),
		LocalOffsets:       make([]uint32, 65),
		LiveLocalMasks:     []uint64{1},
		LiveCallLocalMasks: []uint64{2},
		LiveMaskExtraWords: []uint64{1, 1},
	}
	if !plan.ValidLiveMasks() {
		t.Fatal("valid 65-root two-word plan rejected")
	}
	if !plan.LocalLiveAt(0, 0) || !plan.LocalLiveAt(0, 64) || plan.LocalLiveAt(0, 1) {
		t.Fatal("allocation mask lookup returned the wrong roots")
	}
	if !plan.CallLocalLiveAt(0, 1) || !plan.CallLocalLiveAt(0, 64) || plan.CallLocalLiveAt(0, 0) {
		t.Fatal("call mask lookup returned the wrong roots")
	}
	var allocations, calls []int
	if !plan.VisitLiveLocals(0, false, func(root int) { allocations = append(allocations, root) }) ||
		!plan.VisitLiveLocals(0, true, func(root int) { calls = append(calls, root) }) {
		t.Fatal("valid live-mask iteration rejected")
	}
	if !slices.Equal(allocations, []int{0, 64}) || !slices.Equal(calls, []int{1, 64}) {
		t.Fatalf("live-mask iteration = allocations %v, calls %v", allocations, calls)
	}
	plan.LiveMaskExtraWords = plan.LiveMaskExtraWords[:1]
	if plan.ValidLiveMasks() {
		t.Fatal("truncated two-word allocation arena accepted")
	}
	if plan.VisitLiveLocals(0, true, func(int) {}) {
		t.Fatal("truncated call-mask arena accepted by iteration")
	}
}

func TestGCFrameRootPlanTrackedVectorMask(t *testing.T) {
	const roots = 1025
	extraWords := (roots+63)/64 - 1
	plan := &GCFrameRootPlan{
		LocalOffsets:       make([]uint32, roots),
		LiveLocalMasks:     []uint64{0},
		LiveMaskExtraWords: make([]uint64, extraWords),
	}
	plan.LiveMaskExtraWords[extraWords-1] = 1
	if !plan.ValidLiveMasks() || !plan.LocalLiveAt(0, roots-1) {
		t.Fatal("bounded vector mask lost its highest root")
	}
	plan.LocalOffsets = make([]uint32, GCFrameTrackedLocalLimit+1)
	if plan.ValidLiveMasks() {
		t.Fatal("over-limit vector mask accepted")
	}
}

func TestGCFrameRootPlanTracksWideLocalPopulation(t *testing.T) {
	// Even indexes retain a wide population while leaving in-range misses to
	// exercise both outcomes across the configured uint16 local-index space.
	indexes := make([]uint32, (GCFrameTrackedLocalLimit+1)/2)
	for i := range indexes {
		indexes[i] = uint32(i * 2)
	}
	plan := &GCFrameRootPlan{Candidate: true, LocalIndexes: indexes}
	for _, index := range []uint32{0, 2, GCFrameTrackedLocalLimit - 1} {
		if !plan.TracksLocal(index) {
			t.Fatalf("retained local %d not found", index)
		}
	}
	for _, index := range []uint32{1, 3, GCFrameTrackedLocalLimit - 2} {
		if plan.TracksLocal(index) {
			t.Fatalf("unretained local %d found", index)
		}
	}
	plan.Candidate = false
	if plan.TracksLocal(0) {
		t.Fatal("non-candidate plan reported a retained local")
	}
}

var gcFrameVisitSink int

func BenchmarkGCFrameRootPlanVisitsDisjointWideSites(b *testing.B) {
	const roots = 10_000
	extraPerSite := (roots+63)/64 - 1
	plan := GCFrameRootPlan{
		Candidate:          true,
		LocalIndexes:       make([]uint32, roots),
		LocalOffsets:       make([]uint32, roots),
		LiveLocalMasks:     make([]uint64, roots),
		LiveCallLocalMasks: make([]uint64, roots),
		LiveMaskExtraWords: make([]uint64, roots*extraPerSite*2),
	}
	for root := 0; root < roots; root++ {
		plan.LocalIndexes[root], plan.LocalOffsets[root] = uint32(root), uint32(root*8)
		if root < 64 {
			plan.LiveLocalMasks[root] = uint64(1) << uint(root)
			plan.LiveCallLocalMasks[root] = uint64(1) << uint(root)
		} else {
			word := root/64 - 1
			plan.LiveMaskExtraWords[root*extraPerSite+word] = uint64(1) << uint(root%64)
			plan.LiveMaskExtraWords[(roots+root)*extraPerSite+word] = uint64(1) << uint(root%64)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(roots*2, "sites/op")
	for i := 0; i < b.N; i++ {
		sum := 0
		visit := func(root int) { sum += root }
		for site := 0; site < roots; site++ {
			if !plan.VisitLiveLocals(site, false, visit) || !plan.VisitLiveLocals(site, true, visit) {
				b.Fatal("valid sparse site rejected")
			}
		}
		gcFrameVisitSink = sum
	}
}
