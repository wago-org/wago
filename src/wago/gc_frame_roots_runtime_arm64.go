//go:build (linux || darwin) && arm64

package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// gcHelperRoots publishes exact arm64 native roots. The arm64
// host-call stub saves SP without pushing a return address; SP is therefore the
// function's stable post-prologue frame base. Exact direct callsites use the
// saved LR above each callee frame record to continue through recursive callers.
func (in *Instance) gcHelperRoots(ctrl uintptr, state *gcPublicState, safepointID uint32) gc.RootSet {
	plan := in.c.genericGCFrameRoots()
	if plan == nil || ctrl == 0 {
		return gc.EmptyRoots{}
	}
	var safepoint *compiledGCFrameSafepoint
	for i := range plan.safepoints {
		if plan.safepoints[i].id == safepointID {
			safepoint = &plan.safepoints[i]
			break
		}
	}
	if safepointID == 0 || safepoint == nil {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root safepoint %d is unavailable", safepointID)})
	}
	offsets := safepoint.offsets
	frameBytes := safepoint.frameBytes
	if state == nil || len(offsets) > gcNativeFrameRootLimit || frameBytes < shared.ARM64FrameHeaderBytes {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC arm64 frame-root metadata is unavailable or oversized")})
	}
	ctrlHead := unsafe.Slice((*byte)(offHeapPtr(ctrl+abi.SyncHostCallSavedNativeSPOffset)), 8)
	base := uintptr(binary.LittleEndian.Uint64(ctrlHead))
	if base == 0 {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root control has invalid saved SP %#x", base)})
	}
	for _, off := range offsets {
		if off < shared.ARM64FrameHeaderBytes || off%8 != 0 || off > frameBytes-8 || base > ^uintptr(0)-uintptr(off) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root offset %d is outside frame size %d", off, frameBytes)})
		}
		word := unsafe.Slice((*byte)(offHeapPtr(base+uintptr(off))), 8)
		bits := binary.LittleEndian.Uint64(word)
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root offset %d contains non-compact reference %#x", off, bits)})
		}
	}
	// Cross-instance arm64 GC calls remain outside the admitted product. A return
	// PC outside this module is therefore the native-entry termination boundary.
	state.frameRoots.owner = nil
	state.frameRoots.base = base
	state.frameRoots.offsets = offsets
	state.frameRoots.frameBytes = frameBytes
	state.frameRoots.frameLayout = gcNativeFrameLayoutARM64 // saved LR follows saved FP above the frame reserve
	state.frameRoots.codeBase = in.base
	state.frameRoots.codeBytes = uintptr(len(in.c.Code))
	state.frameRoots.adapterReturnOffsets = plan.adapterReturnOffsets
	state.frameRoots.callsites = plan.callsites
	state.frameRoots.suspended = state
	state.frameRoots.tableRoots = nil
	return &state.frameRoots
}
