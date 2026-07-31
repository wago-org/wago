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
	if state == nil || len(offsets) > gcNativeFrameRootLimit || frameBytes < shared.AMD64FrameHeaderBytes {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root metadata is unavailable or oversized")})
	}
	ctrlHead := unsafe.Slice((*byte)(offHeapPtr(ctrl+abi.SyncHostCallSavedNativeSPOffset)), 8)
	savedRSP := uintptr(binary.LittleEndian.Uint64(ctrlHead))
	if savedRSP == 0 || savedRSP > ^uintptr(0)-abi.AMD64CallReturnAddressBytes {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC frame-root control has invalid saved RSP %#x", savedRSP)})
	}
	base := savedRSP + abi.AMD64CallReturnAddressBytes
	for _, off := range offsets {
		if off < shared.AMD64FrameHeaderBytes || off%8 != 0 || off > frameBytes-8 || base > ^uintptr(0)-uintptr(off) {
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
	state.frameRoots.codeBase = in.base
	state.frameRoots.codeBytes = uintptr(len(in.c.Code))
	state.frameRoots.adapterReturnOffsets = plan.adapterReturnOffsets
	state.frameRoots.callsites = plan.callsites
	state.frameRoots.suspended = state
	if in.c.HasTable && (in.c.TableType == ValAnyRef || in.c.TableType == ValI31Ref) {
		state.tableRoots = gcNativeTableRoots{desc: in.tableDescPtr, bytes: uintptr(in.tableDescLen)}
		state.frameRoots.tableRoots = &state.tableRoots
	} else {
		state.frameRoots.tableRoots = nil
	}
	return &state.frameRoots
}
