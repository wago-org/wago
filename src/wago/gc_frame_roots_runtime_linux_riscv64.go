//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (in *Instance) gcCollectFrameRoots(public *gcPublicState) gcNativeFrameRoots {
	return gcCollectFrameRoots(in, public, gcNativeFrameLayoutRISCV64, true)
}

// gcHelperRoots publishes exact RV64 native roots while a synchronous helper
// has parked the guest. The RV64 park frame records the synchronous helper
// thunk's SP. The thunk owns one fixed call-frame header below the calling Wasm
// frame, so compiler root offsets start after that header. Unlike AMD64 and
// ARM64, each generated Wasm frame stores its return address in word zero.
func (in *Instance) gcHelperRoots(ctrl uintptr, state *gcPublicState, safepointID uint32, stack *coreruntime.Engine) gc.RootSet {
	plan := in.c.genericGCFrameRoots()
	if plan == nil {
		// The pinned ref.eq product roots every earlier allocation in its table
		// before the next helper. Preserve its persistent-domain root publication
		// until it migrates to exact frame maps.
		if state != nil && rv64OfficialGCRefEq(in.c) {
			state.frameRoots = gcCollectFrameRoots(in, state, gcNativeFrameLayoutRISCV64, true)
			return &state.frameRoots
		}
		return gc.EmptyRoots{}
	}
	if ctrl == 0 {
		return gc.EmptyRoots{}
	}
	safepoint := plan.safepointByID(safepointID)
	if safepointID == 0 || safepoint == nil {
		panic(gcHelperFailuref("generic GC frame-root safepoint %d is unavailable", safepointID))
	}
	offsets := safepoint.offsets
	frameBytes := safepoint.frameBytes
	if state == nil || frameBytes < 8 || !validGCFrameOffsets(offsets, frameBytes) {
		panic(gcHelperFailuref("generic GC RV64 frame-root metadata is unavailable or malformed"))
	}
	ctrlHead := unsafe.Slice((*byte)(offHeapPtr(ctrl+abi.SyncHostCallSavedNativeSPOffset)), 8)
	base := uintptr(binary.LittleEndian.Uint64(ctrlHead))
	if base == 0 || base > ^uintptr(0)-shared.RV64CallFrameHeaderBytes {
		panic(gcHelperFailuref("generic GC frame-root control has invalid saved SP %#x", base))
	}
	base += shared.RV64CallFrameHeaderBytes
	if !gcNativeFrameFitsStack(base, frameBytes, stack) {
		panic(gcHelperFailuref("generic GC native frame exceeds stack bounds"))
	}
	for _, off := range offsets {
		if off%8 != 0 || off > frameBytes-8 || base > ^uintptr(0)-uintptr(off) {
			panic(gcHelperFailuref("generic GC frame-root offset %d is outside frame size %d", off, frameBytes))
		}
		word := unsafe.Slice((*byte)(offHeapPtr(base+uintptr(off))), 8)
		bits := binary.LittleEndian.Uint64(word)
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			panic(gcHelperFailuref("generic GC frame-root offset %d contains non-compact reference %#x", off, bits))
		}
	}
	state.frameRoots.owner = in
	state.frameRoots.physicalStack = stack
	state.frameRoots.base = base
	state.frameRoots.offsets = offsets
	state.frameRoots.frameBytes = frameBytes
	state.frameRoots.frameLayout = gcNativeFrameLayoutRISCV64 | gcNativeFrameSyncGlobalRoots
	state.frameRoots.allowExternalReturn = true
	state.frameRoots.codeBase = in.base
	state.frameRoots.codeBytes = uintptr(len(in.c.code))
	state.frameRoots.adapterReturnOffsets = plan.adapterReturnOffsets
	state.frameRoots.callsites = plan.callsites
	state.frameRoots.suspended = state
	return &state.frameRoots
}
