//go:build linux && amd64

package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

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
	var offsets []uint32
	found := false
	for i := range plan.safepoints {
		if plan.safepoints[i].id == safepointID {
			offsets = plan.safepoints[i].offsets
			found = true
			break
		}
	}
	if safepointID == 0 || !found {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root safepoint %d is unavailable", safepointID)})
	}
	if state == nil || len(offsets) > gcNativeFrameRootLimit || plan.frameBytes < shared.AMD64FrameHeaderBytes {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root metadata is unavailable or oversized")})
	}
	ctrlHead := unsafe.Slice((*byte)(offHeapPtr(ctrl+abi.SyncHostCallSavedNativeSPOffset)), 8)
	savedRSP := uintptr(binary.LittleEndian.Uint64(ctrlHead))
	if savedRSP == 0 || savedRSP > ^uintptr(0)-abi.AMD64CallReturnAddressBytes {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root control has invalid saved RSP %#x", savedRSP)})
	}
	base := savedRSP + abi.AMD64CallReturnAddressBytes
	for _, off := range offsets {
		if off < shared.AMD64FrameHeaderBytes || off%8 != 0 || off > plan.frameBytes-8 || base > ^uintptr(0)-uintptr(off) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root offset %d is outside frame size %d", off, plan.frameBytes)})
		}
		word := unsafe.Slice((*byte)(offHeapPtr(base+uintptr(off))), 8)
		bits := binary.LittleEndian.Uint64(word)
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root offset %d contains non-compact reference %#x", off, bits)})
		}
	}
	state.frameRoots.base = base
	state.frameRoots.offsets = offsets
	state.frameRoots.frameBytes = plan.frameBytes
	state.frameRoots.codeBase = in.base
	state.frameRoots.codeBytes = uintptr(len(in.c.Code))
	state.frameRoots.adapterReturnOffset = plan.adapterReturnOffset
	state.frameRoots.callsites = plan.callsites
	return &state.frameRoots
}
