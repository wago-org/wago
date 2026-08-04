package wago

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// hostControlInstances maps stable off-heap control-frame addresses to the
// physically live instance that owns the frame. Native code publishes the exact
// active frame when it parks, so cross-instance host dispatch uses the callee's
// import namespace and HostModule rather than the public root's.
var hostControlInstances sync.Map // map[uintptr]*Instance

func registerHostControl(in *Instance) error {
	if in == nil || len(in.ctrl) < coreruntime.HostCtrlFrameBytes {
		return fmt.Errorf("invalid synchronous host control frame")
	}
	ptr := offHeapSlicePtr(in.ctrl)
	if _, loaded := hostControlInstances.LoadOrStore(ptr, in); loaded {
		return fmt.Errorf("duplicate synchronous host control frame %x", ptr)
	}
	return nil
}

func unregisterHostControl(in *Instance) {
	if in == nil || len(in.ctrl) == 0 {
		return
	}
	ptr := offHeapSlicePtr(in.ctrl)
	if current, ok := hostControlInstances.Load(ptr); ok && current == in {
		hostControlInstances.Delete(ptr)
	}
}

func offHeapSlicePtr(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}

// dispatchSynchronousHostCall routes the common root-instance host call without
// touching the process-wide registry. A cross-instance callee falls back to the
// active control-frame lookup published by its native host stub.
func (root *Instance) dispatchSynchronousHostCall(ctrl uintptr, importIdx uint32, args, results []uint64) {
	active := root
	if root == nil || ctrl != offHeapSlicePtr(root.ctrl) {
		value, ok := hostControlInstances.Load(ctrl)
		if !ok {
			panic(invalidHostReference{err: fmt.Errorf("host control frame %x has no live instance", ctrl)})
		}
		active, ok = value.(*Instance)
		if !ok || active == nil {
			panic(invalidHostReference{err: fmt.Errorf("host control frame %x has no live instance", ctrl)})
		}
	}
	if active.hostCall == nil {
		panic(invalidHostReference{err: fmt.Errorf("host control frame %x has no dispatcher", ctrl)})
	}
	if importIdx&gcStructDispatchBit != 0 {
		// Internal GC helpers cannot re-enter Wasm or arbitrary host code. Keep the
		// native execution lease while operating on the parked frame instead of
		// paying the public host-call release/reacquire protocol at every GC opcode.
		// Dispatch directly here: routing through hostCall would repeat the GC-bit
		// branch and add an indirect closure call on every helper transition.
		if importIdx&hostFuncRefDispatchBit != 0 {
			panic(gcStructHelperError{err: fmt.Errorf("invalid overlapping GC/host dispatch index %#x", importIdx)})
		}
		if active.gc != nil {
			helper, safepoint := shared.DecodeGCDispatch(importIdx &^ gcStructDispatchBit)
			active.dispatchGCHelperParked(ctrl, helper, safepoint, args, results)
			return
		}
		// Preserve the injected dispatcher path used by hardening tests and by a
		// partially constructed instance so missing-collector diagnostics remain
		// centralized in the configured host dispatcher.
		active.hostCall(ctrl, importIdx, args, results)
		return
	}

	// Run arbitrary Go host code without the non-reentrant native execution
	// lease. The deferred reacquire covers normal return, HostExit, validation
	// panics, and arbitrary host panics. Rebind the exact parked callee because a
	// nested wasm entry may have replaced its shared basedata context.
	activation := active.pushGCHostActivation(ctrl, importIdx)
	if err := active.rootGCHostArguments(activation, importIdx, args); err != nil {
		active.clearGCHostResultRoots(activation)
		active.popGCHostActivation(activation)
		panic(invalidHostReference{err: err})
	}
	epoch := nativeExecutionEpoch
	nativeExecutionMu.Unlock()
	// Keep the parked activation and any GC host-result roots published until the
	// native execution lease is reacquired. A competing entry may collect while
	// arbitrary host code runs, but cannot observe the unrooted handoff window
	// between host result validation and the caller's resumed native frame.
	defer active.popGCHostActivation(activation)
	defer func() {
		nativeExecutionMu.Lock()
		active.clearGCHostResultRoots(activation)
		// If no nested or competing public entry ran while host code owned the Go
		// stack, the parked callee's basedata is still installed. Avoid rewriting
		// all eight context words on this overwhelmingly common return path.
		if nativeExecutionEpoch != epoch {
			if err := active.bindNativeContext(); err != nil {
				panic(invalidHostReference{err: err})
			}
		}
	}()
	active.hostCall(ctrl, importIdx, args, results)
}
