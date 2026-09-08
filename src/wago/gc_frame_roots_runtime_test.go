package wago

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCCollectFrameRootsPreservesArchitectureBoundary(t *testing.T) {
	in := &Instance{}
	state := &gcPublicState{}
	roots := gcCollectFrameRoots(in, state, gcNativeFrameLayoutARM64, true)
	if roots.owner != in || roots.suspended != state || roots.frameLayout != gcNativeFrameLayoutARM64 || !roots.allowExternalReturn {
		t.Fatalf("collect roots = %+v, want ARM64 owner/suspension boundary", roots)
	}
}

func TestGCCollectFrameRootsUsesCurrentArchitecture(t *testing.T) {
	if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
		t.Skipf("native GC frame roots are unavailable on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	wantLayout, wantExternal := uint8(gcNativeFrameLayoutAMD64), true
	switch runtime.GOARCH {
	case "amd64":
	case "arm64":
		wantLayout = gcNativeFrameLayoutARM64
	default:
		t.Skipf("native GC frame roots are unavailable on %s", runtime.GOARCH)
	}
	in := &Instance{}
	state := &gcPublicState{}
	roots := in.gcCollectFrameRoots(state)
	if roots.owner != in || roots.suspended != state || roots.frameLayout != wantLayout || roots.allowExternalReturn != wantExternal {
		t.Fatalf("collect roots = %+v, want layout=%d external=%v", roots, wantLayout, wantExternal)
	}
}

func TestGCNativeFrameRootsARM64FrameRecordWalk(t *testing.T) {
	frame := make([]byte, 160)
	code := make([]byte, 256)
	base := uintptr(unsafe.Pointer(&frame[0]))
	codeBase := uintptr(unsafe.Pointer(&code[0]))

	const (
		calleeFrameBytes = 32
		callerBase       = 48 // callee frame + 16-byte FP/LR record
		callerFrameBytes = 64
	)
	binary.LittleEndian.PutUint64(frame[16:], 7)
	binary.LittleEndian.PutUint64(frame[calleeFrameBytes+8:], uint64(codeBase+100))
	binary.LittleEndian.PutUint64(frame[callerBase+24:], 11)
	binary.LittleEndian.PutUint64(frame[callerBase+callerFrameBytes+8:], uint64(codeBase+200))

	roots := gcNativeFrameRoots{
		base:                 base,
		offsets:              []uint32{16},
		frameBytes:           calleeFrameBytes,
		frameLayout:          gcNativeFrameLayoutARM64,
		codeBase:             codeBase,
		codeBytes:            uintptr(len(code)),
		adapterReturnOffsets: []uint32{200},
		callsites: []compiledGCFrameCallsite{{
			returnOffset: 100,
			frameBytes:   callerFrameBytes,
			offsets:      []uint32{24},
		}},
	}
	seen := 0
	roots.RangeRoots(func(slot gc.RootSlot) bool {
		seen++
		slot.SetRef(slot.GetRef() + 2)
		return true
	})
	if seen != 2 {
		t.Fatalf("root count = %d, want 2", seen)
	}
	if got := (*gc.Root)(offHeapPtr(base + 16)).GetRef(); got != 9 {
		t.Fatalf("callee root = %d, want 9", got)
	}
	if got := (*gc.Root)(offHeapPtr(base + callerBase + 24)).GetRef(); got != 13 {
		t.Fatalf("caller root = %d, want 13", got)
	}
	runtime.KeepAlive(frame)
	runtime.KeepAlive(code)
}

func TestGCNativeFrameRootsARM64ForeignWrapperStackAdjustment(t *testing.T) {
	frame := make([]byte, 256)
	code := make([]byte, 256)
	base := uintptr(unsafe.Pointer(&frame[0]))
	codeBase := uintptr(unsafe.Pointer(&code[0]))

	const (
		calleeFrameBytes = 32
		wrapperSaveBytes = 64
		callerBase       = calleeFrameBytes + shared.ARM64FrameRecordBytes + wrapperSaveBytes
		callerFrameBytes = 64
	)
	binary.LittleEndian.PutUint64(frame[16:], 3)
	binary.LittleEndian.PutUint64(frame[calleeFrameBytes+shared.ARM64SavedLROffset:], uint64(codeBase+80))
	binary.LittleEndian.PutUint64(frame[callerBase+24:], 17)
	binary.LittleEndian.PutUint64(frame[callerBase+callerFrameBytes+shared.ARM64SavedLROffset:], uint64(codeBase+200))

	roots := gcNativeFrameRoots{
		base:                 base,
		offsets:              []uint32{16},
		frameBytes:           calleeFrameBytes,
		frameLayout:          gcNativeFrameLayoutARM64,
		codeBase:             codeBase,
		codeBytes:            uintptr(len(code)),
		adapterReturnOffsets: []uint32{200},
		callsites: []compiledGCFrameCallsite{{
			returnOffset: 80,
			frameBytes:   callerFrameBytes,
			stackAdjust:  wrapperSaveBytes,
			offsets:      []uint32{24},
		}},
	}
	seen := 0
	roots.RangeRoots(func(slot gc.RootSlot) bool {
		seen++
		slot.SetRef(slot.GetRef() + 1)
		return true
	})
	if seen != 2 {
		t.Fatalf("root count = %d, want 2", seen)
	}
	if got := (*gc.Root)(offHeapPtr(base + callerBase + 24)).GetRef(); got != 18 {
		t.Fatalf("adjusted caller root = %d, want 18", got)
	}
	runtime.KeepAlive(frame)
	runtime.KeepAlive(code)
}

func TestGCModuleFrameRootPlanAllowsMultipleNativePathsPerCall(t *testing.T) {
	plan := &shared.GCFrameRootPlan{
		Candidate:  true,
		Exact:      true,
		FrameBytes: 64,
		Locals:     []shared.GCFrameLocal{{Index: 0, Offset: 16}},
	}
	if !plan.SetLiveMasks([]uint64{1, 1}, 1, 1) {
		t.Fatal("failed to set live masks")
	}
	for _, site := range [][2]uint32{{4, 0}, {8, 0}, {12, 64}} {
		if !plan.AppendCallsite(site[0], site[1], []uint32{16}) {
			t.Fatal("failed to append callsite")
		}
	}
	if !plan.AppendSafepoint([]uint32{16}) {
		t.Fatal("failed to append safepoint")
	}
	if !validGCModuleFrameRootPlan(gcModuleFrameRootPlanForTest(t, plan)) {
		t.Fatal("one logical call with three native return paths was rejected")
	}
}

func TestGCModuleFrameRootPlanRejectsPendingFunction(t *testing.T) {
	module := shared.NewGCModuleFrameRootPlan(1)
	if !module.MarkFunction(0) {
		t.Fatal("failed to mark collecting function")
	}
	if validGCModuleFrameRootPlan(module) {
		t.Fatal("module with an unpopulated collecting function was accepted")
	}
}

func TestValidGCFrameLocalsRequiresStrictIndexOrder(t *testing.T) {
	for _, locals := range [][]shared.GCFrameLocal{
		{{Index: 2, Offset: 16}, {Index: 2, Offset: 24}},
		{{Index: 3, Offset: 16}, {Index: 1, Offset: 24}},
	} {
		if validGCFrameLocals(locals, 64) {
			t.Fatalf("validGCFrameLocals accepted unsorted indexes: %#v", locals)
		}
	}
}

func TestGCModuleFrameRootPlanDerivesDenseSafepointIDs(t *testing.T) {
	plan := func(base uint32) *shared.GCFrameRootPlan {
		plan := &shared.GCFrameRootPlan{
			Candidate:     true,
			Exact:         true,
			FrameBytes:    8,
			SafepointBase: base,
		}
		if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
			t.Fatal("failed to set live masks")
		}
		if !plan.AppendSafepoint(nil) {
			t.Fatal("failed to append safepoint")
		}
		return plan
	}
	if !validGCModuleFrameRootPlan(gcModuleFrameRootPlanForTest(t, plan(shared.GCSafepointIDMax-1))) {
		t.Fatal("maximum derived safepoint ID was rejected")
	}
	if validGCModuleFrameRootPlan(gcModuleFrameRootPlanForTest(t, plan(shared.GCSafepointIDMax))) {
		t.Fatal("derived safepoint ID above the dispatch domain was accepted")
	}
	if validGCModuleFrameRootPlan(gcModuleFrameRootPlanForTest(t, plan(0), plan(0))) {
		t.Fatal("overlapping derived safepoint ID ranges were accepted")
	}
}

func gcModuleFrameRootPlanForTest(t testing.TB, plans ...*shared.GCFrameRootPlan) *shared.GCModuleFrameRootPlan {
	t.Helper()
	module := shared.NewGCModuleFrameRootPlan(len(plans))
	count := 0
	for function, plan := range plans {
		if plan != nil {
			if !module.MarkFunction(function) {
				t.Fatalf("mark function root plan %d", function)
			}
			count++
		}
	}
	if !module.ReserveFunctions(count) {
		t.Fatalf("reserve %d function root plans", count)
	}
	for function, plan := range plans {
		if plan != nil {
			dst, ok := module.BeginFunction(function)
			if !ok {
				t.Fatalf("begin function root plan %d", function)
			}
			*dst = *plan
		}
	}
	return module
}

func TestNormalizeAdapterReturnOffsets(t *testing.T) {
	got := normalizeAdapterReturnOffsets([]uint32{300, 100, 300, 200, 100})
	want := []uint32{100, 200, 300}
	if !slices.Equal(got, want) {
		t.Fatalf("normalized adapter return offsets = %v, want %v", got, want)
	}
}

func TestGCNativeFrameRootsARM64ExternalReturnTerminates(t *testing.T) {
	frame := make([]byte, 64)
	code := make([]byte, 64)
	base := uintptr(unsafe.Pointer(&frame[0]))
	codeBase := uintptr(unsafe.Pointer(&code[0]))
	binary.LittleEndian.PutUint64(frame[16:], 5)
	binary.LittleEndian.PutUint64(frame[32+8:], uint64(codeBase+uintptr(len(code))+4096))

	roots := gcNativeFrameRoots{
		base:        base,
		offsets:     []uint32{16},
		frameBytes:  32,
		frameLayout: gcNativeFrameLayoutARM64,
		codeBase:    codeBase,
		codeBytes:   uintptr(len(code)),
		callsites:   []compiledGCFrameCallsite{{returnOffset: 8, frameBytes: 32}},
	}
	seen := 0
	roots.RangeRoots(func(gc.RootSlot) bool {
		seen++
		return true
	})
	if seen != 1 {
		t.Fatalf("root count = %d, want 1", seen)
	}
	runtime.KeepAlive(frame)
	runtime.KeepAlive(code)
}

func TestCompiledGCFrameRootsSafepointByID(t *testing.T) {
	plan := &compiledGCFrameRoots{safepoints: []compiledGCFrameSafepoint{
		{id: 1, frameBytes: 32},
		{id: 2, frameBytes: 40},
		{id: 4, frameBytes: 48},
	}}
	for _, tc := range []struct {
		id   uint32
		want uint32
		ok   bool
	}{
		{id: 0},
		{id: 1, want: 32, ok: true},
		{id: 2, want: 40, ok: true},
		{id: 3},
		{id: 4, want: 48, ok: true},
		{id: 5},
	} {
		got := plan.safepointByID(tc.id)
		if !tc.ok {
			if got != nil {
				t.Fatalf("safepointByID(%d) = %+v, want nil", tc.id, got)
			}
			continue
		}
		if got == nil || got.id != tc.id || got.frameBytes != tc.want {
			t.Fatalf("safepointByID(%d) = %+v, want id=%d frameBytes=%d", tc.id, got, tc.id, tc.want)
		}
	}
}

type gcCountingRootRefSink struct {
	count     int
	sum       uint64
	stopAfter int
}

func (s *gcCountingRootRefSink) VisitRootRef(ref gc.Ref) bool {
	s.count++
	s.sum += uint64(ref)
	return s.stopAfter == 0 || s.count < s.stopAfter
}

func TestGCNativeFrameRootWideEnumerationIsAllocationFree(t *testing.T) {
	const rootsN = 4096
	frame, releaseFrame := newGCNativeTestFrame(t, shared.AMD64FrameHeaderBytes+rootsN*8)
	defer releaseFrame()
	offsets := make([]uint32, rootsN)
	for i := range offsets {
		offsets[i] = uint32(shared.AMD64FrameHeaderBytes + i*8)
		binary.LittleEndian.PutUint64(frame[offsets[i]:], uint64(gc.I31New(int32(i))))
	}
	roots := gcNativeFrameRoots{base: uintptr(unsafe.Pointer(&frame[0])), offsets: offsets, frameBytes: uint32(len(frame))}
	sink := new(gcCountingRootRefSink)
	if got := testing.AllocsPerRun(100, func() {
		sink.count, sink.sum = 0, 0
		roots.RangeRootRefs(sink)
		// Keep the mapping descriptor live across each probe. Unix tests use the
		// same off-heap ownership class as production native frames, avoiding a
		// Go 1.22 stack relocation behind roots' intentional uintptr ABI base.
		runtime.KeepAlive(frame)
	}); got != 0 {
		t.Fatalf("wide root enumeration allocations=%v, want 0", got)
	}
	if sink.count != rootsN || sink.sum == 0 {
		t.Fatalf("wide root enumeration count/sum=%d/%d", sink.count, sink.sum)
	}
	sink.count, sink.sum, sink.stopAfter = 0, 0, 3
	if roots.RangeRootRefs(sink) || sink.count != sink.stopAfter {
		t.Fatalf("stopped root enumeration continued: count=%d result=true", sink.count)
	}
	runtime.KeepAlive(frame)
}

func BenchmarkGCNativeFrameRootEnumerationWidths(b *testing.B) {
	for _, rootsN := range []int{64, 65, 128, 256, 1024} {
		b.Run(fmt.Sprintf("roots=%d", rootsN), func(b *testing.B) {
			frame := make([]byte, shared.AMD64FrameHeaderBytes+rootsN*8)
			offsets := make([]uint32, rootsN)
			for i := range offsets {
				offsets[i] = uint32(shared.AMD64FrameHeaderBytes + i*8)
				binary.LittleEndian.PutUint64(frame[offsets[i]:], uint64(gc.I31New(int32(i))))
			}
			roots := gcNativeFrameRoots{base: uintptr(unsafe.Pointer(&frame[0])), offsets: offsets, frameBytes: uint32(len(frame))}
			sink := new(gcCountingRootRefSink)
			b.ReportAllocs()
			b.ReportMetric(float64(rootsN), "roots/op")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sink.count, sink.sum = 0, 0
				roots.RangeRootRefs(sink)
			}
			if sink.count != rootsN || sink.sum == 0 {
				b.Fatalf("root enumeration count/sum=%d/%d", sink.count, sink.sum)
			}
			runtime.KeepAlive(frame)
		})
	}
}

func BenchmarkCompiledGCFrameRootsSafepointByIDDense(b *testing.B) {
	const count = 4096
	plan := &compiledGCFrameRoots{safepoints: make([]compiledGCFrameSafepoint, count)}
	for i := range plan.safepoints {
		plan.safepoints[i].id = uint32(i + 1)
	}
	b.ReportAllocs()
	var got *compiledGCFrameSafepoint
	for i := 0; i < b.N; i++ {
		got = plan.safepointByID(uint32(i%count + 1))
	}
	if got == nil {
		b.Fatal("dense lookup returned nil")
	}
}
