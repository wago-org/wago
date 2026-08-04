package wago

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime/gc"
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
	wantLayout, wantExternal := uint8(gcNativeFrameLayoutAMD64), false
	switch runtime.GOARCH {
	case "amd64":
	case "arm64":
		wantLayout, wantExternal = gcNativeFrameLayoutARM64, true
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
		Candidate:          true,
		Exact:              true,
		FrameBytes:         64,
		LiveCallLocalMasks: []uint64{1},
		LocalIndexes:       []uint32{0},
		LocalOffsets:       []uint32{16},
		Safepoints: []shared.GCFrameSafepointPlan{{
			ID:      1,
			Offsets: []uint32{16},
		}},
		LiveLocalMasks: []uint64{1},
		Callsites: []shared.GCFrameCallsitePlan{
			{ReturnOffset: 4, Offsets: []uint32{16}},
			{ReturnOffset: 8, Offsets: []uint32{16}},
			{ReturnOffset: 12, StackAdjust: 64, Offsets: []uint32{16}},
		},
	}
	if !validGCModuleFrameRootPlan(&shared.GCModuleFrameRootPlan{Functions: []*shared.GCFrameRootPlan{plan}}) {
		t.Fatal("one logical call with three native return paths was rejected")
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
