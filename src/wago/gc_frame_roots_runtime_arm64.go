//go:build (linux || darwin) && arm64

package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

func (in *Instance) gcCollectFrameRoots(public *gcPublicState) gcNativeFrameRoots {
	return gcCollectFrameRoots(in, public, gcNativeFrameLayoutARM64, true)
}

// gcHelperRoots publishes exact arm64 native roots. The arm64
// host-call stub saves SP without pushing a return address; SP is therefore the
// function's stable post-prologue frame base. Exact direct callsites use the
// saved LR above each callee frame record to continue through recursive callers.
func (in *Instance) gcHelperRoots(ctrl uintptr, state *gcPublicState, safepointID uint32) gc.RootSet {
	if ctrl == 0 {
		return gc.EmptyRoots{}
	}
	control := unsafe.Slice((*byte)(offHeapPtr(ctrl)), abi.ARM64SyncHostCallSavedBytes)
	returnPC := uintptr(binary.LittleEndian.Uint64(control[abi.ARM64SyncHostCallSavedLROffset:]))
	compiled, codeBase := in.compilerGenerationForPC(returnPC)
	if compiled == nil {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC arm64 helper return PC %#x has no compiler generation", returnPC)})
	}
	plan := compiled.genericGCFrameRoots()
	if plan == nil {
		return gc.EmptyRoots{}
	}
	safepoint := plan.safepointByID(safepointID)
	if safepointID == 0 || safepoint == nil {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root safepoint %d is unavailable", safepointID)})
	}
	offsets := safepoint.offsets
	frameBytes := safepoint.frameBytes
	if state == nil || frameBytes < 8 || !validGCFrameOffsets(offsets, frameBytes) {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC arm64 frame-root metadata is unavailable or malformed")})
	}
	ctrlHead := unsafe.Slice((*byte)(offHeapPtr(ctrl+abi.SyncHostCallSavedNativeSPOffset)), 8)
	base := uintptr(binary.LittleEndian.Uint64(ctrlHead))
	if base == 0 {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root control has invalid saved SP %#x", base)})
	}
	for _, off := range offsets {
		if off%8 != 0 || off > frameBytes-8 || base > ^uintptr(0)-uintptr(off) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root offset %d is outside frame size %d", off, frameBytes)})
		}
		word := unsafe.Slice((*byte)(offHeapPtr(base+uintptr(off))), 8)
		bits := binary.LittleEndian.Uint64(word)
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root offset %d contains non-compact reference %#x", off, bits)})
		}
	}
	state.frameRoots.owner = in
	state.frameRoots.base = base
	state.frameRoots.offsets = offsets
	state.frameRoots.frameBytes = frameBytes
	state.frameRoots.frameLayout = gcNativeFrameLayoutARM64 | gcNativeFrameSyncGlobalRoots // saved LR follows saved FP above the frame reserve
	state.frameRoots.allowExternalReturn = true                                            // non-register public entries return directly to enterNative
	state.frameRoots.codeBase = codeBase
	state.frameRoots.codeBytes = uintptr(len(compiled.code))
	state.frameRoots.adapterReturnOffsets = plan.adapterReturnOffsets
	state.frameRoots.callsites = plan.callsites
	state.frameRoots.suspended = state
	return &state.frameRoots
}
