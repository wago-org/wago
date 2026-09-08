//go:build linux && amd64

package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (in *Instance) gcCollectFrameRoots(public *gcPublicState) gcNativeFrameRoots {
	// Public entries return through enterNative into Go after the outermost
	// generated frame. That external return is the valid end of the root chain.
	return gcCollectFrameRoots(in, public, gcNativeFrameLayoutAMD64, true)
}

// gcHelperRoots publishes exact-typed collector-reference locals from the
// parked native frame. hostCallStub saves RSP while it points at the helper-call
// return address, so the function's stable frame base is savedRSP+8. The root
// slots point directly at the off-heap frame, allowing collector rewrites even
// though the current compact handle representation is stable.
func (in *Instance) gcHelperRoots(ctrl uintptr, state *gcPublicState, safepointID uint32) gc.RootSet {
	plan := in.c.genericGCFrameRoots()
	if plan == nil || ctrl == 0 {
		return gc.EmptyRoots{}
	}
	safepoint := plan.safepointByID(safepointID)
	if safepointID == 0 || safepoint == nil {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root safepoint %d is unavailable", safepointID)})
	}
	offsets := safepoint.offsets
	frameBytes := safepoint.frameBytes
	if state == nil || frameBytes < 8 || !validGCFrameOffsets(offsets, frameBytes) {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root metadata is unavailable or malformed")})
	}
	ctrlHead := unsafe.Slice((*byte)(offHeapPtr(ctrl+abi.SyncHostCallSavedNativeSPOffset)), 8)
	savedRSP := uintptr(binary.LittleEndian.Uint64(ctrlHead))
	if savedRSP == 0 || savedRSP > ^uintptr(0)-abi.AMD64CallReturnAddressBytes {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root control has invalid saved RSP %#x", savedRSP)})
	}
	base := savedRSP + abi.AMD64CallReturnAddressBytes
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
	state.frameRoots.frameLayout = gcNativeFrameLayoutAMD64 | gcNativeFrameSyncGlobalRoots
	state.frameRoots.codeBase = in.base
	state.frameRoots.codeBytes = uintptr(len(in.c.code))
	state.frameRoots.adapterReturnOffsets = plan.adapterReturnOffsets
	state.frameRoots.callsites = plan.callsites
	state.frameRoots.suspended = state
	return &state.frameRoots
}
