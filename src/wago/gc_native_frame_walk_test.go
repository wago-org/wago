//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
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
	case "riscv64":
		wantLayout = gcNativeFrameLayoutRISCV64
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

func TestGCNativeFrameRootsRISCV64SavedRAWalk(t *testing.T) {
	frame, releaseFrame := newGCNativeTestFrame(t, 128)
	defer releaseFrame()
	code, releaseCode := newGCNativeTestFrame(t, 256)
	defer releaseCode()
	base := uintptr(unsafe.Pointer(&frame[0]))
	codeBase := uintptr(unsafe.Pointer(&code[0]))

	const (
		calleeFrameBytes = 32
		callerBase       = calleeFrameBytes
		callerFrameBytes = 64
	)
	// RA is word zero of each RV64 frame. Roots begin after the fixed header.
	binary.LittleEndian.PutUint64(frame[0:], uint64(codeBase+100))
	binary.LittleEndian.PutUint64(frame[16:], 7)
	binary.LittleEndian.PutUint64(frame[callerBase:], uint64(codeBase+200))
	binary.LittleEndian.PutUint64(frame[callerBase+24:], 11)

	roots := gcNativeFrameRoots{
		base:                 base,
		offsets:              []uint32{16},
		frameBytes:           calleeFrameBytes,
		frameLayout:          gcNativeFrameLayoutRISCV64,
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

func TestGCNativeFrameRootsRISCV64ForeignBridgeStackAdjustment(t *testing.T) {
	frame, releaseFrame := newGCNativeTestFrame(t, 256)
	defer releaseFrame()
	code, releaseCode := newGCNativeTestFrame(t, 256)
	defer releaseCode()
	base := uintptr(unsafe.Pointer(&frame[0]))
	codeBase := uintptr(unsafe.Pointer(&code[0]))

	const (
		calleeFrameBytes = 32
		bridgeFrameBytes = 64
		callerBase       = calleeFrameBytes + bridgeFrameBytes
		callerFrameBytes = 64
	)
	// The target callee returns into the bridge allocation. Its callsite map
	// skips that allocation and resumes at the consumer Wasm frame.
	binary.LittleEndian.PutUint64(frame[0:], uint64(codeBase+80))
	binary.LittleEndian.PutUint64(frame[16:], 3)
	binary.LittleEndian.PutUint64(frame[callerBase:], uint64(codeBase+200))
	binary.LittleEndian.PutUint64(frame[callerBase+24:], 17)

	roots := gcNativeFrameRoots{
		base:                 base,
		offsets:              []uint32{16},
		frameBytes:           calleeFrameBytes,
		frameLayout:          gcNativeFrameLayoutRISCV64,
		codeBase:             codeBase,
		codeBytes:            uintptr(len(code)),
		adapterReturnOffsets: []uint32{200},
		callsites: []compiledGCFrameCallsite{{
			returnOffset: 80,
			frameBytes:   callerFrameBytes,
			stackAdjust:  bridgeFrameBytes,
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

func TestGCNativeFrameRootsCallsiteFreeForeignTargetReturnsToCallerOwner(t *testing.T) {
	frame := make([]byte, 256)
	providerCode := make([]byte, 256)
	consumerCode := make([]byte, 256)
	base := uintptr(unsafe.Pointer(&frame[0]))
	providerCodeBase := uintptr(unsafe.Pointer(&providerCode[0]))
	consumerCodeBase := uintptr(unsafe.Pointer(&consumerCode[0]))

	const (
		providerFrameBytes = 32
		bridgeFrameBytes   = 64
		consumerBase       = providerFrameBytes + bridgeFrameBytes
		consumerFrameBytes = 64
	)
	binary.LittleEndian.PutUint64(frame[0:], uint64(consumerCodeBase+80))
	binary.LittleEndian.PutUint64(frame[16:], 3)
	binary.LittleEndian.PutUint64(frame[consumerBase:], uint64(consumerCodeBase+200))
	binary.LittleEndian.PutUint64(frame[consumerBase+24:], 17)

	collector := new(gc.Collector)
	domain := &gcStoreDomain{collector: collector}
	providerRoots := &compiledGCFrameRoots{}
	consumerRoots := &compiledGCFrameRoots{
		adapterReturnOffsets: []uint32{200},
		callsites: []compiledGCFrameCallsite{{
			returnOffset: 80,
			frameBytes:   consumerFrameBytes,
			stackAdjust:  bridgeFrameBytes,
			offsets:      []uint32{24},
		}},
	}
	provider := &Instance{c: &Compiled{code: providerCode, validateMemo: &validateMemo{compiledGCFrameRootState: testCompiledGCFrameRootState(providerRoots)}}, gc: collector, base: providerCodeBase}
	consumer := &Instance{c: &Compiled{code: consumerCode, validateMemo: &validateMemo{compiledGCFrameRootState: testCompiledGCFrameRootState(consumerRoots)}}, gc: collector, base: consumerCodeBase}
	store := &referenceStore{
		instances:          map[*Instance]*referenceStoreInstance{provider: {}, consumer: {}},
		gcDomainStoreState: testGCDomainStoreState(t, &gcDomainTopology{first: domain, last: domain, n: 1}),
	}
	provider.refStore, consumer.refStore = store, store

	roots := gcNativeFrameRoots{
		owner:       provider,
		base:        base,
		offsets:     []uint32{16},
		frameBytes:  providerFrameBytes,
		frameLayout: gcNativeFrameLayoutRISCV64,
		codeBase:    providerCodeBase,
		codeBytes:   uintptr(len(providerCode)),
		callsites:   providerRoots.callsites,
	}
	seen := 0
	if !roots.rangeChain(func(slot gc.RootSlot) bool {
		seen++
		slot.SetRef(slot.GetRef() + 1)
		return true
	}, nil, nil) {
		t.Fatal("root traversal stopped early")
	}
	if seen != 2 {
		t.Fatalf("root count = %d, want 2", seen)
	}
	if got := (*gc.Root)(offHeapPtr(base + consumerBase + 24)).GetRef(); got != 18 {
		t.Fatalf("foreign caller root = %d, want 18", got)
	}
	runtime.KeepAlive(frame)
	runtime.KeepAlive(providerCode)
	runtime.KeepAlive(consumerCode)
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

func TestGCNativeFrameRootsDeepInBoundsChain(t *testing.T) {
	const frames = 4098
	const frameStride = 32 + shared.ARM64FrameRecordBytes
	frame := make([]byte, frames*frameStride)
	code := make([]byte, 256)
	base := uintptr(unsafe.Pointer(&frame[0]))
	codeBase := uintptr(unsafe.Pointer(&code[0]))
	for i := 0; i < frames; i++ {
		binary.LittleEndian.PutUint64(frame[i*frameStride+16:], 1)
		returnPC := codeBase + 100
		if i == frames-1 {
			returnPC = codeBase + 200
		}
		binary.LittleEndian.PutUint64(frame[i*frameStride+32+shared.ARM64SavedLROffset:], uint64(returnPC))
	}
	roots := gcNativeFrameRoots{
		base:                 base,
		offsets:              []uint32{16},
		frameBytes:           32,
		frameLayout:          gcNativeFrameLayoutARM64,
		codeBase:             codeBase,
		codeBytes:            uintptr(len(code)),
		adapterReturnOffsets: []uint32{200},
		callsites:            []compiledGCFrameCallsite{{returnOffset: 100, frameBytes: 32, offsets: []uint32{16}}},
	}
	defer runtime.KeepAlive(frame)
	defer runtime.KeepAlive(code)
	defer func() {
		if failure := recover(); failure != nil {
			t.Errorf("in-bounds frame chain panicked: %v", failure)
		}
	}()
	seen := 0
	roots.RangeRoots(func(gc.RootSlot) bool {
		seen++
		return true
	})
	if seen != frames {
		t.Fatalf("root count = %d, want %d", seen, frames)
	}
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

func BenchmarkScalingReturnPCLookup(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			frame := make([]byte, 160)
			code := make([]byte, 16*(n+2))
			base := uintptr(unsafe.Pointer(&frame[0]))
			codeBase := uintptr(unsafe.Pointer(&code[0]))
			callsites := make([]compiledGCFrameCallsite, n)
			for i := range callsites {
				callsites[i] = compiledGCFrameCallsite{returnOffset: uint32(16 * (i + 1)), frameBytes: 32}
			}
			binary.LittleEndian.PutUint64(frame[40:], uint64(codeBase+uintptr(16*n)))
			binary.LittleEndian.PutUint64(frame[88:], uint64(codeBase+uintptr(16*(n+1))))
			roots := gcNativeFrameRoots{base: base, frameBytes: 32, frameLayout: gcNativeFrameLayoutARM64, codeBase: codeBase, codeBytes: uintptr(len(code)), adapterReturnOffsets: []uint32{uint32(16 * (n + 1))}, callsites: callsites}
			visit := func(gc.RootSlot) bool { return true }
			b.ReportAllocs()
			b.ResetTimer()
			for k := 0; k < b.N; k++ {
				roots.RangeRoots(visit)
			}
			runtime.KeepAlive(frame)
			runtime.KeepAlive(code)
		})
	}
}
