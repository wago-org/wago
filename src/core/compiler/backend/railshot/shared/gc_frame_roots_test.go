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
	if got := unsafe.Sizeof(GCFrameRootPlan{}); got != 136 {
		t.Fatalf("GCFrameRootPlan size=%d, want 136", got)
	}
	if got := unsafe.Sizeof(GCModuleFrameRootPlan{}); got != 48 {
		t.Fatalf("GCModuleFrameRootPlan size=%d, want 48", got)
	}
}

func TestGCModuleFrameRootPlanSparseOwnership(t *testing.T) {
	module := NewGCModuleFrameRootPlan(4)
	if !module.MarkFunction(1) || !module.MarkFunction(3) || !module.FunctionPending(1) || module.FunctionPending(2) || !module.ReserveFunctions(2) {
		t.Fatal("failed to prepare sparse function plans")
	}
	first, firstOK := module.BeginFunction(1)
	second, secondOK := module.BeginFunction(3)
	if !firstOK || !secondOK {
		t.Fatal("failed to add sparse function plans")
	}
	*first = GCFrameRootPlan{Candidate: true, FrameBytes: 16}
	*second = GCFrameRootPlan{Candidate: true, FrameBytes: 32}
	if module.FunctionCount() != 4 || module.Function(0) != nil || module.Function(2) != nil ||
		module.Function(1).FrameBytes != 16 || module.Function(3).FrameBytes != 32 {
		t.Fatalf("sparse module plan lookup is invalid")
	}
	if _, ok := module.BeginFunction(1); ok {
		t.Fatal("duplicate function plan was accepted")
	}
	if _, ok := module.BeginFunction(4); ok {
		t.Fatal("duplicate or out-of-range function plan was accepted")
	}
	if module.FunctionPending(1) || module.FunctionPending(3) {
		t.Fatal("populated function plans remain pending")
	}
}

func TestGCModuleFrameRootPlanBeginRequiresReservation(t *testing.T) {
	module := NewGCModuleFrameRootPlan(2)
	if !module.MarkFunction(0) || !module.MarkFunction(1) || !module.ReserveFunctions(1) {
		t.Fatal("failed to prepare under-reserved module plan")
	}
	first, ok := module.BeginFunction(0)
	if !ok {
		t.Fatal("first reserved function plan was rejected")
	}
	if _, ok := module.BeginFunction(1); ok {
		t.Fatal("function plan growth beyond the stable reservation was accepted")
	}
	first.FrameBytes = 24
	if module.Function(0) != first || module.Function(0).FrameBytes != 24 {
		t.Fatal("reserved function plan address was not stable")
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

func TestGCFrameSafepointStreamRetainsFixedPrefix(t *testing.T) {
	plan := GCFrameRootPlan{Exact: true}
	if !plan.SetFixedOffsets([]uint32{8, 24}) || !slices.Equal(plan.FixedOffsets(), []uint32{8, 24}) {
		t.Fatalf("fixed roots = %#v", plan.FixedOffsets())
	}
	if !plan.AppendSafepoint([]uint32{16}) {
		t.Fatal("failed to append safepoint after fixed roots")
	}
	fixed := plan.FixedOffsets()
	fixed = append(fixed, 32)
	if !slices.Equal(fixed, []uint32{8, 24, 32}) {
		t.Fatalf("appended fixed-root view = %#v", fixed)
	}
	if !plan.VisitSafepoints(func(_ int, offsets []uint32) bool {
		return slices.Equal(offsets, []uint32{16})
	}) {
		t.Fatal("fixed root prefix was parsed as or overwrote safepoint data")
	}
	plan.ResetSafepoints()
	if !plan.Exact || plan.SafepointCount() != 0 || !slices.Equal(plan.FixedOffsets(), []uint32{8, 24}) || len(plan.SafepointData) != 2 {
		t.Fatalf("reset plan = exact %v count %d fixed %#v data %#v", plan.Exact, plan.SafepointCount(), plan.FixedOffsets(), plan.SafepointData)
	}
	if plan.SetFixedOffsets([]uint32{32}) {
		t.Fatal("fixed roots were replaced after ownership transfer")
	}
	plan.fixedOffsetCount = uint32(len(plan.SafepointData) + 1)
	if plan.VisitSafepoints(func(int, []uint32) bool { return true }) {
		t.Fatal("malformed fixed root prefix was accepted")
	}
	plan.ResetSafepoints()
	if plan.Exact {
		t.Fatal("malformed fixed root prefix survived reset")
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
		Locals:              make([]GCFrameLocal, 65),
		liveMaskWords:       []uint64{1, 1, 2, 1},
		allocationMaskCount: 1,
		callMaskCount:       1,
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
	plan.liveMaskWords = plan.liveMaskWords[:3]
	if plan.ValidLiveMasks() {
		t.Fatal("truncated two-word allocation arena accepted")
	}
	if plan.VisitLiveLocals(0, true, func(int) {}) {
		t.Fatal("truncated call-mask arena accepted by iteration")
	}
	plan.allocationMaskCount = 4
	if plan.CallLocalLiveAt(0, 0) {
		t.Fatal("out-of-range call-mask offset was accepted")
	}
}

func TestGCFrameRootPlanTrackedVectorMask(t *testing.T) {
	const roots = 1025
	wordCount := (roots + 63) / 64
	plan := &GCFrameRootPlan{
		Locals:              make([]GCFrameLocal, roots),
		liveMaskWords:       make([]uint64, wordCount),
		allocationMaskCount: 1,
	}
	plan.liveMaskWords[wordCount-1] = 1
	if !plan.ValidLiveMasks() || !plan.LocalLiveAt(0, roots-1) {
		t.Fatal("bounded vector mask lost its highest root")
	}
	plan.Locals = make([]GCFrameLocal, GCFrameTrackedLocalLimit+1)
	if plan.ValidLiveMasks() {
		t.Fatal("over-limit vector mask accepted")
	}
}

func TestGCFrameRootPlanTracksWideLocalPopulation(t *testing.T) {
	// Even indexes retain a wide population while leaving in-range misses to
	// exercise both outcomes across the configured uint16 local-index space.
	locals := make([]GCFrameLocal, (GCFrameTrackedLocalLimit+1)/2)
	for i := range locals {
		locals[i].Index = uint32(i * 2)
	}
	plan := &GCFrameRootPlan{Candidate: true, Locals: locals}
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
	wordCount := (roots + 63) / 64
	plan := GCFrameRootPlan{
		Candidate:           true,
		Locals:              make([]GCFrameLocal, roots),
		liveMaskWords:       make([]uint64, roots*wordCount*2),
		allocationMaskCount: roots,
		callMaskCount:       roots,
	}
	for root := 0; root < roots; root++ {
		plan.Locals[root] = GCFrameLocal{Index: uint32(root), Offset: uint32(root * 8)}
		word := root / 64
		plan.liveMaskWords[root*wordCount+word] = uint64(1) << uint(root%64)
		plan.liveMaskWords[(roots+root)*wordCount+word] = uint64(1) << uint(root%64)
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
