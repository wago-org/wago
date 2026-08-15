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

// hostInvocationContexts carries the public root's callback identity across a
// native cross-instance transfer. The control-frame address is unique to the
// parked activation and lets the producer's bound host dispatcher construct a
// HostModule authorized by the invocation that actually owns the GC lease.
type hostInvocationContext struct {
	id          invocationID
	reservation *pluginOperationReservation
}

var hostInvocationContexts sync.Map // map[uintptr]hostInvocationContext

func currentHostInvocationContext(ctrl uintptr, in *Instance) hostInvocationContext {
	if value, ok := hostInvocationContexts.Load(ctrl); ok {
		return value.(hostInvocationContext)
	}
	if in != nil {
		if id := in.currentInvocationID(); id != 0 {
			return hostInvocationContext{id: id, reservation: currentInvocationReservation(in)}
		}
	}
	return hostInvocationContext{}
}

func activeHostInvocationContext(in *Instance) hostInvocationContext {
	if in == nil {
		return hostInvocationContext{}
	}
	return currentHostInvocationContext(offHeapSlicePtr(in.ctrl), in)
}

func bindHostInvocationContext(ctrl uintptr, next hostInvocationContext) func() {
	if ctrl == 0 || next.id == 0 {
		return func() {}
	}
	previous, loaded := hostInvocationContexts.Load(ctrl)
	hostInvocationContexts.Store(ctrl, next)
	return func() {
		if loaded {
			hostInvocationContexts.Store(ctrl, previous)
		} else {
			hostInvocationContexts.Delete(ctrl)
		}
	}
}

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
	stackTop := active.eng.StackTop()
	if root != nil && root.eng != nil {
		// Cross-instance native calls remain on the public root's foreign stack
		// even though the parked control frame and callsite map belong to active.
		stackTop = root.eng.StackTop()
	}
	activation := active.pushGCHostActivation(ctrl, importIdx, stackTop)
	if err := active.rootGCHostArguments(activation, importIdx, args); err != nil {
		active.clearGCHostResultRoots(activation)
		active.popGCHostActivation(activation)
		panic(invalidHostReference{err: err})
	}
	// Cross-instance native dispatch parks the producer while the public root still
	// owns the invocation identity and collector lease. Carry that identity into
	// the producer's HostModule instead of reading its zero local invocation ID.
	invocation := activeHostInvocationContext(root)
	if invocation.id == 0 {
		invocation = activeHostInvocationContext(active)
	}
	id := invocation.id
	// Exact parked native roots and translated GC host arguments are now
	// published. Release every collector lease owned by the public native root
	// while arbitrary host code runs. A non-GC relay may have pre-acquired more
	// imported GC domains than the active producer whose frame parked here.
	leaseOwner := active
	if root != nil && root.executionFlags.Load()&executionFlagImportedGCDomain != 0 {
		leaseOwner = root
	}
	suspendedGCInvocation := leaseOwner.suspendGCInvocation(id)
	var localMu *sync.Mutex
	var epoch uint64
	if active.usesIndependentExecution() {
		localMu = active.independentNativeExecutionMu()
		localMu.Unlock()
	} else {
		epoch = nativeExecutionEpoch
		nativeExecutionMu.Unlock()
	}
	// Keep the parked activation and any GC host-result roots published until the
	// native execution lease is reacquired. A competing entry may collect while
	// arbitrary host code runs, but cannot observe the unrooted handoff window
	// between host result validation and the caller's resumed native frame.
	defer active.popGCHostActivation(activation)
	defer func() {
		suspendedGCInvocation.resume()
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
			if err := active.jm.RebindTrapCell(active.trap); err != nil {
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
	markNativeActiveID(active, id)
	defer unmarkNativeActiveID(active, id)
	if active != root {
		restoreInvocationContext := bindHostInvocationContext(ctrl, invocation)
		defer restoreInvocationContext()
	}
	active.hostCall(ctrl, importIdx, args, results)
}

// prepareHostReentryState gives arbitrary host code an isolated native stack,
// control frame, trap cell, and call scratch. A host function may synchronously
// call this same Instance through a component cycle while the outer activation
// is parked. Reusing any of those buffers corrupts the parked frame and can turn
// an ordinary guest trap into a process fault.
func (in *Instance) prepareHostReentryState() (func(), error) {
	in.lifeMu.Lock()
	invocation := activeHostInvocationContext(in)
	eng, err := coreruntime.AcquireEngine()
	if err != nil {
		in.lifeMu.Unlock()
		return nil, fmt.Errorf("acquire host re-entry engine: %w", err)
	}
	ctrl := make([]byte, coreruntime.HostCtrlFrameBytes)
	if err := coreruntime.InitHostCtrlFrame(ctrl); err != nil {
		_ = coreruntime.ReleaseEngine(eng)
		in.lifeMu.Unlock()
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
		in.lifeMu.Unlock()
		return nil, err
	}
	restoreInvocationContext := bindHostInvocationContext(offHeapSlicePtr(ctrl), invocation)
	// Cross-instance and imported-host dispatch descriptors restore their target
	// from this stable context image. Publish the activation's private control
	// frame there as well as in basedata, otherwise a nested import switches back
	// to the instance's original frame and overwrites the parked outer activation.
	nativeCtx := unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), coreruntime.InstanceContextBytes)
	outerCustomCtx := binary.LittleEndian.Uint64(nativeCtx)
	binary.LittleEndian.PutUint64(nativeCtx, uint64(offHeapSlicePtr(ctrl)))
	in.lifeMu.Unlock()

	return func() {
		in.lifeMu.Lock()
		binary.LittleEndian.PutUint64(nativeCtx, outerCustomCtx)
		restoreInvocationContext()
		unregisterHostControl(in)
		in.eng, in.ctrl = outerEngine, outerCtrl
		in.serArgs, in.results, in.trap = outerArgs, outerResults, outerTrap
		in.resultVals = outerResultVals
		in.ic, in.icNext = outerInvokeCache, outerInvokeCacheNext
		in.instructionState = outerInstructionState
		if err := coreruntime.ReleaseEngine(eng); err != nil {
			in.lifeMu.Unlock()
			panic(invalidHostReference{err: fmt.Errorf("release host re-entry engine: %w", err)})
		}
		in.lifeMu.Unlock()
	}, nil
}
