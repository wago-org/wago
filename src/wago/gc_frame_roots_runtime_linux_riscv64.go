//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (in *Instance) gcCollectFrameRoots(public *gcPublicState) gcNativeFrameRoots {
	roots := gcCollectFrameRoots(in, public, gcNativeFrameLayoutRISCV64, true)
	roots.domainHostRoots = true
	return roots
}

// gcHelperRoots publishes exact RV64 native roots while a synchronous helper
// has parked the guest. The RV64 park frame records the synchronous helper
// thunk's SP. The thunk owns one fixed call-frame header below the calling Wasm
// frame, so compiler root offsets start after that header. Unlike AMD64 and
// ARM64, each generated Wasm frame stores its return address in word zero.
func (in *Instance) gcHelperRoots(ctrl uintptr, state *gcPublicState, safepointID uint32, stack *coreruntime.Engine) gc.RootSet {
	plan := in.c.genericGCFrameRoots()
	if plan == nil {
		if rv64GCStructBasic(in.c) || rv64GCArrayFixedOfficial(in.c) || rv64GCArrayDefaultOfficial(in.c) || rv64GCArrayPackedData(in.c) {
			return in.gcBoundedCallerRoots(ctrl, state, stack)
		}
		// The pinned ref.eq product roots every earlier allocation in its table
		// before the next helper. Preserve its persistent-domain root publication
		// until it migrates to exact frame maps.
		if state != nil && rv64OfficialGCRefEq(in.c) {
			state.frameRoots = in.gcCollectFrameRoots(state)
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
	if state == nil || frameBytes < 8 || !validGCFrameOffsetsKinds(offsets, frameBytes, in.c.functionEnvironmentFrameRoots(), in.c.compactExternFrameRoots()) {
		panic(gcHelperFailuref("generic GC RV64 frame-root metadata is unavailable or malformed"))
	}
	if ctrl > ^uintptr(0)-abi.SyncHostCallSavedNativeSPOffset {
		panic(gcHelperFailuref("generic GC frame-root control address overflows"))
	}
	// The parked control can be the retained host-control block rather than a
	// stack allocation. Its lifetime and identity are checked by helper dispatch.
	base := uintptr(nativeGCFrameWord(ctrl + abi.SyncHostCallSavedNativeSPOffset))
	if base == 0 || base > ^uintptr(0)-shared.RV64CallFrameHeaderBytes {
		panic(gcHelperFailuref("generic GC frame-root control has invalid saved SP %#x", base))
	}
	base += shared.RV64CallFrameHeaderBytes
	if !gcNativeFrameFitsStack(base, frameBytes, stack) {
		panic(gcHelperFailuref("generic GC native frame exceeds stack bounds"))
	}
	frame := stack.NativeStackRange(base, int(frameBytes))
	if uint64(len(frame)) != uint64(frameBytes) {
		panic(gcHelperFailuref("generic GC native frame has no owned stack range"))
	}
	for _, encoded := range offsets {
		off := shared.GCFrameRootOffset(encoded)
		if off%8 != 0 || off > frameBytes-8 || base > ^uintptr(0)-uintptr(off) {
			panic(gcHelperFailuref("generic GC frame-root offset %d is outside frame size %d", off, frameBytes))
		}
		bits := binary.LittleEndian.Uint64(frame[off:])
		if encoded&shared.GCFrameExternRefRootFlag != 0 {
			continue // opaque word is resolved to its shell by the root walker
		}
		if encoded&shared.GCFrameFuncRefRootFlag != 0 {
			if bits != 0 && managedFunctionDescriptorOwner(in, bits) == nil {
				panic(gcHelperFailuref("function frame root has no live physical owner"))
			}
			continue
		}
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
	state.frameRoots.domainHostRoots = true
	return &state.frameRoots
}

// The exact products have no collector references live across their own
// allocation checkpoints. Their native import activation can still sit below
// a collecting caller, whose authenticated return map must remain live.
func (in *Instance) gcBoundedCallerRoots(ctrl uintptr, state *gcPublicState, stack *coreruntime.Engine) gc.RootSet {
	if state == nil || stack == nil || in.refStore == nil {
		panic(gcHelperFailuref("bounded GC allocation has no retained root owner"))
	}
	top, bytes := stack.StackTop(), uintptr(stack.StackBytes())
	limit := stack.StackLimit()
	if bytes == 0 || top < bytes || top < abi.RV64InvocationViewBytes || limit < top-bytes || limit >= top {
		panic(gcHelperFailuref("bounded GC allocation has no physical stack"))
	}
	state.frameRoots = gcCollectFrameRoots(in, state, gcNativeFrameLayoutRISCV64, true)
	state.frameRoots.domainHostRoots = true
	view, targetContext := in.jm.RV64InvocationView(), in.nativeContext
	var owner *Instance
	var pc, base uintptr
	for view != top-abi.RV64InvocationViewBytes {
		callerPC, home, context, callerBase, parentView, valid := railshot.NativeImportHostContinuation(ctrl, targetContext, view, limit, top)
		if !valid {
			panic(gcHelperFailuref("bounded GC allocation has no authenticated native caller activation"))
		}
		owner = in.refStore.gcFrameOwner(callerPC, in.gc)
		if owner == nil || owner.jm == nil || owner.jm.LinMemBase() != home || owner.nativeContext != context {
			owner = in.refStore.gcNativeCallerOwner(callerPC, home, context)
		}
		if owner == nil || owner.jm == nil || owner.jm.LinMemBase() != home || owner.nativeContext != context {
			panic(gcHelperFailuref("bounded GC allocation has no authenticated collector caller"))
		}
		if owner.gc == in.gc {
			pc, base = callerPC, callerBase
			break
		}
		// A different collector cannot contain this domain's compact objects.
		// The parent view is strictly above this complete activation record.
		view, targetContext = parentView, context
		ctrl = railshot.NativeImportParentHostControlInStack(view, limit, top)
	}
	if view == top-abi.RV64InvocationViewBytes {
		return &state.frameRoots
	}
	plan := owner.c.genericGCFrameRoots()
	if plan == nil {
		panic(gcHelperFailuref("bounded GC allocation collector caller has no root maps"))
	}
	var callsite *compiledGCFrameCallsite
	for i := range plan.callsites {
		if uintptr(plan.callsites[i].returnOffset) == pc-owner.base {
			callsite = &plan.callsites[i]
			break
		}
	}
	if callsite == nil || base > ^uintptr(0)-uintptr(callsite.stackAdjust) {
		panic(gcHelperFailuref("bounded GC allocation caller has no exact return map"))
	}
	base += uintptr(callsite.stackAdjust)
	if !gcNativeFrameFitsStack(base, callsite.frameBytes, stack) {
		panic(gcHelperFailuref("bounded GC allocation caller frame exceeds stack bounds"))
	}
	state.frameRoots.base, state.frameRoots.offsets, state.frameRoots.frameBytes = base, callsite.offsets, callsite.frameBytes
	state.frameRoots.codeBase, state.frameRoots.codeBytes = owner.base, uintptr(len(owner.c.code))
	state.frameRoots.adapterReturnOffsets, state.frameRoots.callsites = plan.adapterReturnOffsets, plan.callsites
	state.frameRoots.physicalStack = stack
	return &state.frameRoots
}
