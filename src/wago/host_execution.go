package wago

import (
	"encoding/binary"
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
	if importIdx&shared.AtomicWaitDispatchBit != 0 {
		if importIdx&(gcStructDispatchBit|hostFuncRefDispatchBit) != 0 {
			panic(atomicWaitHelperError{err: fmt.Errorf("invalid overlapping atomic helper dispatch index %#x", importIdx)})
		}
		active.dispatchAtomicWaitHelper(importIdx&^shared.AtomicWaitDispatchBit, args, results)
		return
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
	var localMu *sync.Mutex
	epoch := nativeExecutionEpoch
	if active.usesIndependentExecution() {
		localMu = active.independentNativeExecutionMu()
		localMu.Unlock()
	} else {
		nativeExecutionMu.Unlock()
	}
	// Keep the parked activation and any GC host-result roots published until the
	// native execution lease is reacquired. A competing entry may collect while
	// arbitrary host code runs, but cannot observe the unrooted handoff window
	// between host result validation and the caller's resumed native frame.
	defer active.popGCHostActivation(activation)
	defer func() {
		if localMu != nil {
			localMu.Lock()
		} else {
			nativeExecutionMu.Lock()
		}
		active.clearGCHostResultRoots(activation)
		// Public calls on one instance are serialized, but synchronous host code
		// may re-enter the parked instance while its local lease is released.
		// Always restore local context; the process lease can retain its epoch
		// shortcut because competing entries advance the shared epoch.
		if localMu != nil || nativeExecutionEpoch != epoch {
			if err := active.bindNativeContext(); err != nil {
				panic(invalidHostReference{err: err})
			}
			active.jm.SetStackFence(active.eng.StackLimit())
			if err := active.jm.BindTrapCell(active.trap); err != nil {
				panic(invalidHostReference{err: err})
			}
			// bindNativeContext restores the instance's immutable context image,
			// whose custom-context word names its original control frame. A nested
			// host re-entry has a distinct live frame; republish that exact frame
			// before resuming its parked native activation.
			active.jm.SetCustomCtx(ctrl)
		}
	}()
	// Only arbitrary host code can synchronously re-enter this instance. The
	// active marker includes the callback-scoped invocation identity, so another
	// call chain cannot masquerade as this parked activation.
	markNativeActive(active)
	defer unmarkNativeActive(active)
	active.hostCall(ctrl, importIdx, args, results)
}

// prepareHostReentryState gives arbitrary host code an isolated native stack,
// control frame, trap cell, and call scratch. A host function may synchronously
// call this same Instance through a component cycle while the outer activation
// is parked. Reusing any of those buffers corrupts the parked frame and can turn
// an ordinary guest trap into a process fault.
func (in *Instance) prepareHostReentryState() (func(), error) {
	eng, err := coreruntime.AcquireEngine()
	if err != nil {
		return nil, fmt.Errorf("acquire host re-entry engine: %w", err)
	}
	ctrl := make([]byte, coreruntime.HostCtrlFrameBytes)
	if err := coreruntime.InitHostCtrlFrame(ctrl); err != nil {
		_ = coreruntime.ReleaseEngine(eng)
		return nil, err
	}

	outerEngine, outerCtrl := in.eng, in.ctrl
	outerArgs, outerResults, outerTrap := in.serArgs, in.results, in.trap
	outerResultVals := in.resultVals
	outerInvokeCache, outerInvokeCacheNext := in.ic, in.icNext
	outerInstructionState := in.instructionState
	in.eng = eng
	in.ctrl = ctrl
	in.serArgs = make([]byte, len(outerArgs))
	in.results = make([]byte, len(outerResults))
	in.trap = make([]byte, len(outerTrap))
	in.resultVals = make([]uint64, len(outerResultVals), cap(outerResultVals))
	in.ic = [4]invokeCache{}
	in.icNext = 0
	in.instructionState = instructionState{}
	if err := registerHostControl(in); err != nil {
		in.eng, in.ctrl = outerEngine, outerCtrl
		in.serArgs, in.results, in.trap = outerArgs, outerResults, outerTrap
		in.resultVals = outerResultVals
		in.ic, in.icNext = outerInvokeCache, outerInvokeCacheNext
		in.instructionState = outerInstructionState
		_ = coreruntime.ReleaseEngine(eng)
		return nil, err
	}
	// Cross-instance and imported-host dispatch descriptors restore their target
	// from this stable context image. Publish the activation's private control
	// frame there as well as in basedata, otherwise a nested import switches back
	// to the instance's original frame and overwrites the parked outer activation.
	nativeCtx := unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), coreruntime.InstanceContextBytes)
	outerCustomCtx := binary.LittleEndian.Uint64(nativeCtx)
	binary.LittleEndian.PutUint64(nativeCtx, uint64(offHeapSlicePtr(ctrl)))

	return func() {
		binary.LittleEndian.PutUint64(nativeCtx, outerCustomCtx)
		unregisterHostControl(in)
		in.eng, in.ctrl = outerEngine, outerCtrl
		in.serArgs, in.results, in.trap = outerArgs, outerResults, outerTrap
		in.resultVals = outerResultVals
		in.ic, in.icNext = outerInvokeCache, outerInvokeCacheNext
		in.instructionState = outerInstructionState
		if err := coreruntime.ReleaseEngine(eng); err != nil {
			panic(invalidHostReference{err: fmt.Errorf("release host re-entry engine: %w", err)})
		}
	}, nil
}
