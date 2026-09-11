package wago

import (
	"context"
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
	parent      context.Context
}

// resolvedHostCall accepts identity derived by the runtime while its native
// lease is held. The callee must not replace the public root ID with its own ID.
type resolvedHostCall func(uintptr, uint32, []uint64, []uint64, hostInvocationContext)

func (c hostInvocationContext) empty() bool {
	return c.id == 0 && c.reservation == nil && c.parent == nil
}

var hostInvocationContexts sync.Map // map[uintptr]hostInvocationContext

// hostLoopActivation belongs to one Go native-entry/host-resume loop. The
// invocation gate keeps the root identity/reservation stable, and the parent
// binding spans this loop. Nested entries get distinct values and control
// frames. This cache is neither execution ownership nor callback authority.
type hostLoopActivation struct {
	root                        *Instance
	ctrl                        uintptr
	invocation                  hostInvocationContext
	state                       *instancePluginState
	parkedNativeContextReusable bool
}

func (a *hostLoopActivation) stateFor(active *Instance) *instancePluginState {
	if active == a.root && a.state != nil {
		return a.state
	}
	state := active.ensurePluginState()
	if active == a.root {
		a.state = state
	}
	return state
}

func (a *hostLoopActivation) context(active *Instance) hostInvocationContext {
	if a.invocation.id != 0 && a.ctrl == offHeapSlicePtr(a.root.ctrl) {
		return a.invocation
	}
	return a.resolveContext(active)
}

func (a *hostLoopActivation) resolveContext(active *Instance) hostInvocationContext {
	invocation := activeHostInvocationContext(a.root)
	if a.root != nil && a.ctrl != 0 && a.ctrl == offHeapSlicePtr(a.root.ctrl) && invocation.id != 0 {
		a.invocation = invocation
	}
	if invocation.empty() {
		// An active callee's identity must never become a cached public root.
		invocation = activeHostInvocationContext(active)
	}
	return invocation
}

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
	if ctrl == 0 || next.empty() {
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

func bindHostInvocationParent(in *Instance, parent context.Context) func() {
	if in == nil {
		return func() {}
	}
	ctrl := offHeapSlicePtr(in.ctrl)
	_, inherited := hostInvocationContexts.Load(ctrl)
	if parent == nil && !inherited {
		return func() {}
	}
	invocation := currentHostInvocationContext(ctrl, in)
	invocation.parent = parent
	return bindHostInvocationContext(ctrl, invocation)
}

func registerHostControl(in *Instance) error {
	if in == nil || len(in.ctrl) < coreruntime.HostCtrlFrameBytes {
		return fmt.Errorf("invalid synchronous host control frame")
	}
	ptr := offHeapSlicePtr(in.ctrl)
	if _, loaded := hostControlInstances.LoadOrStore(ptr, in); loaded {
		return fmt.Errorf("duplicate synchronous host control frame %x", ptr)
	}
	if err := coreruntime.RegisterHostCtrlFrame(in.ctrl); err != nil {
		hostControlInstances.Delete(ptr)
		return fmt.Errorf("register runtime synchronous host control frame: %w", err)
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
	coreruntime.UnregisterHostCtrlFrame(in.ctrl)
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
	activation := hostLoopActivation{root: root}
	if root != nil {
		activation.ctrl = offHeapSlicePtr(root.ctrl)
	}
	activation.dispatch(ctrl, importIdx, args, results)
}

func (a *hostLoopActivation) dispatch(ctrl uintptr, importIdx uint32, args, results []uint64) {
	root := a.root
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
		active.hostCall(ctrl, importIdx, args, results, hostInvocationContext{})
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
	invocation := a.context(active)
	// Exact parked native roots and translated GC host arguments are now
	// published. Release every collector lease owned by the public native root
	// while arbitrary host code runs. A non-GC relay may have pre-acquired more
	// imported GC domains than the active producer whose frame parked here.
	leaseOwner := active
	if root != nil && root.executionFlags.Load()&executionFlagImportedGCDomain != 0 {
		leaseOwner = root
	}
	id := invocation.id
	// The no-domain branch does not construct a zero-value suspension. The
	// conditional value stays on this Go frame; only its pointer is captured by
	// cleanup. Keeping the original dispatch frame avoids extra GC-path calls.
	var gcSuspension *gcInvocationSuspension
	if leaseOwner.hostCallNeedsGCSuspension() {
		suspension := leaseOwner.suspendGCInvocation(id)
		gcSuspension = &suspension
	}
	var localMu *sync.Mutex
	var epoch uint64
	var localVersion uint64
	state := a.stateFor(active)
	if active.usesIndependentExecution() {
		if active.memoryDir != nil {
			localMu = &active.memoryDir.nativeMu
		} else {
			localMu = &state.nativeExecutionMu
		}
		localVersion = state.nativeContextVersion.Load()
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
		if gcSuspension != nil {
			gcSuspension.resume()
		}
		if localMu != nil {
			localMu.Lock()
		} else {
			nativeExecutionMu.Lock()
		}
		active.clearGCHostResultRoots(activation)
		// Reuse only private, non-collector context with no intervening native
		// entry or guarded host mutation. Do not read the global epoch under a
		// local lease. All root, interruption, and resume steps remain required.
		restore := false
		if localMu != nil {
			restore = !active.canReuseParkedNativeContextWithState(localVersion, state)
		} else {
			restore = nativeExecutionEpoch != epoch
		}
		if restore {
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
	markNativeActiveState(state, id)
	defer unmarkNativeActiveState(state, id)
	if active != root {
		restoreInvocationContext := bindHostInvocationContext(ctrl, invocation)
		defer restoreInvocationContext()
	}
	active.hostCall(ctrl, importIdx, args, results, invocation)
}

// dispatchTypedScalarPortal is the capability-free root portal. Its callback
// type cannot inspect Caller or obtain supported re-entry authority, and non-GC
// activations have no native roots to publish while parked. Independent
// instances retain their already-exclusive local lease; shared execution
// releases the global lease so another instance may run. The context version or
// global epoch still decides whether native context must be rebound before resume.
func (a *hostLoopActivation) dispatchTypedScalarExpandedPortal(ctrl uintptr, importIdx, rawSlots uint32, a0, a1 uint64) (uint64, bool) {
	active := a.root
	if active == nil || ctrl != a.ctrl ||
		importIdx&hostFuncRefDispatchBit != 0 || int(importIdx) >= len(active.syncHosts) ||
		active.gc != nil || active.executionFlags.Load()&(executionFlagImportedGCDomain|executionFlagDynamicGCDomain|executionFlagStoreOwnedGCCollector) != 0 {
		return 0, false
	}
	binding := &active.syncHosts[importIdx]
	if binding.gate != nil || binding.scalarKind < syncHostTypedI32 {
		return 0, false
	}
	if binding.scalarKind == syncHostTypedI32 {
		if rawSlots != 1|1<<16 {
			return 0, false
		}
	} else if binding.scalarKind == syncHostTypedI32x2 {
		if rawSlots != 2|1<<16 {
			return 0, false
		}
	} else if !binding.matchesTypedScalarSlots(rawSlots) {
		return 0, false
	}

	state := a.state
	flags := active.executionFlags.Load()
	if flags&(executionFlagIndependent|executionFlagNativeControlShared) == executionFlagIndependent {
		version := state.nativeContextVersion.Load()
		var result uint64
		if binding.scalarKind == syncHostTypedI32 {
			result = I32(binding.typedI32(AsI32(a0)))
		} else if binding.scalarKind == syncHostTypedI32x2 {
			result = I32(binding.typedI32x2(AsI32(a0), AsI32(a1)))
		} else {
			result = binding.callTypedScalar(a0, a1)
		}
		if version == ^uint64(0) || !a.parkedNativeContextReusable ||
			active.executionFlags.Load()&(executionFlagIndependent|executionFlagNativeControlShared) != executionFlagIndependent ||
			state.nativeContextVersion.Load() != version {
			active.restoreTypedScalarNativeContext(ctrl)
		}
		return result, true
	}

	epoch := nativeExecutionEpoch
	nativeExecutionMu.Unlock()
	defer func() {
		nativeExecutionMu.Lock()
		if nativeExecutionEpoch != epoch {
			active.restoreTypedScalarNativeContext(ctrl)
		}
	}()

	if binding.scalarKind == syncHostTypedI32 {
		return I32(binding.typedI32(AsI32(a0))), true
	}
	if binding.scalarKind == syncHostTypedI32x2 {
		return I32(binding.typedI32x2(AsI32(a0), AsI32(a1))), true
	}
	return binding.callTypedScalar(a0, a1), true
}

// dispatchTypedScalarPortal preserves the compact one-result ABI and hot path
// for the original typed scalar imports. Expanded signatures use the separate
// portal above so adding them cannot perturb this path's register allocation.
func (a *hostLoopActivation) dispatchTypedScalarPortal(ctrl uintptr, importIdx, rawSlots uint32, a0, a1 uint64) (uint64, bool) {
	active := a.root
	if active == nil || ctrl != a.ctrl ||
		importIdx&hostFuncRefDispatchBit != 0 || int(importIdx) >= len(active.syncHosts) ||
		active.gc != nil || active.executionFlags.Load()&(executionFlagImportedGCDomain|executionFlagDynamicGCDomain|executionFlagStoreOwnedGCCollector) != 0 {
		return 0, false
	}
	binding := &active.syncHosts[importIdx]
	if binding.gate != nil || binding.scalarKind < syncHostTypedI32 {
		return 0, false
	}
	typedScalarArity := uint32(binding.scalarKind - syncHostScalar)
	if rawSlots != typedScalarArity|1<<16 {
		return 0, false
	}

	state := a.state
	flags := active.executionFlags.Load()
	if flags&(executionFlagIndependent|executionFlagNativeControlShared) == executionFlagIndependent {
		version := state.nativeContextVersion.Load()
		var result uint64
		if binding.scalarKind == syncHostTypedI32 {
			result = I32(binding.typedI32(AsI32(a0)))
		} else {
			result = I32(binding.typedI32x2(AsI32(a0), AsI32(a1)))
		}
		if version == ^uint64(0) || !a.parkedNativeContextReusable ||
			active.executionFlags.Load()&(executionFlagIndependent|executionFlagNativeControlShared) != executionFlagIndependent ||
			state.nativeContextVersion.Load() != version {
			active.restoreTypedScalarNativeContext(ctrl)
		}
		return result, true
	}

	epoch := nativeExecutionEpoch
	nativeExecutionMu.Unlock()
	defer func() {
		nativeExecutionMu.Lock()
		if nativeExecutionEpoch != epoch {
			active.restoreTypedScalarNativeContext(ctrl)
		}
	}()

	if binding.scalarKind == syncHostTypedI32 {
		return I32(binding.typedI32(AsI32(a0))), true
	}
	return I32(binding.typedI32x2(AsI32(a0), AsI32(a1))), true
}

func (b *syncHostBinding) matchesTypedScalarSlots(raw uint32) bool {
	switch b.scalarKind {
	case syncHostTypedNone:
		return raw == 0
	case syncHostTypedI32V:
		return raw == 1
	case syncHostTypedI32:
		return raw == 1|1<<16
	case syncHostTypedI32x2V:
		return raw == 2
	case syncHostTypedI32x2:
		return raw == 2|1<<16
	case syncHostTypedI32R2:
		return raw == 1|2<<16
	case syncHostTypedI32x2R2:
		return raw == 2|2<<16
	default:
		return false
	}
}

func (b *syncHostBinding) callTypedScalar(a0, a1 uint64) uint64 {
	switch b.scalarKind {
	case syncHostTypedNone:
		b.typedNone()
	case syncHostTypedI32V:
		b.typedI32V(AsI32(a0))
	case syncHostTypedI32:
		return I32(b.typedI32(AsI32(a0)))
	case syncHostTypedI32x2V:
		b.typedI32x2V(AsI32(a0), AsI32(a1))
	case syncHostTypedI32x2:
		return I32(b.typedI32x2(AsI32(a0), AsI32(a1)))
	case syncHostTypedI32R2:
		a, c := b.typedI32R2(AsI32(a0))
		return uint64(uint32(a)) | uint64(uint32(c))<<32
	case syncHostTypedI32x2R2:
		a, c := b.typedI32x2R2(AsI32(a0), AsI32(a1))
		return uint64(uint32(a)) | uint64(uint32(c))<<32
	}
	return 0
}

func (in *Instance) restoreTypedScalarNativeContext(ctrl uintptr) {
	if err := in.bindNativeContext(); err != nil {
		panic(invalidHostReference{err: err})
	}
	in.jm.SetStackFence(in.eng.StackLimit())
	if err := in.jm.RebindTrapCell(in.trap); err != nil {
		panic(invalidHostReference{err: err})
	}
	in.jm.SetCustomCtx(ctrl)
}

func (in *Instance) hasDirectTypedScalarHost() bool {
	for i := range in.syncHosts {
		binding := &in.syncHosts[i]
		if binding.gate == nil && (binding.scalarKind == syncHostTypedI32 || binding.scalarKind == syncHostTypedI32x2) {
			return true
		}
	}
	return false
}

func (in *Instance) hasExpandedTypedScalarHost() bool {
	for i := range in.syncHosts {
		binding := &in.syncHosts[i]
		if binding.gate == nil && binding.scalarKind >= syncHostTypedNone {
			return true
		}
	}
	return false
}

// prepareHostReentryState gives arbitrary host code an isolated native stack,
// control frame, trap cell, and call scratch. A host function may synchronously
// call this same Instance through a component cycle while the outer activation
// is parked. Reusing any of those buffers corrupts the parked frame and can turn
// an ordinary guest trap into a process fault.
func (in *Instance) prepareHostReentryState() (func(), error) {
	in.invalidateNativeContext()
	in.lifeMu.Lock()
	invocation := activeHostInvocationContext(in)
	stackBytes := coreruntime.DefaultNativeStackBytes
	if in.eng != nil && in.eng.StackBytes() != 0 {
		stackBytes = in.eng.StackBytes()
	}
	eng, err := coreruntime.AcquireEngineWithStackBytes(stackBytes)
	if err != nil {
		in.lifeMu.Unlock()
		return nil, fmt.Errorf("acquire host re-entry engine: %w", err)
	}
	ctrl := make([]byte, len(in.ctrl))
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
		// Close may race after the nested trap cell is installed. The nested
		// activation can consume that interrupt while unwinding; carry the close
		// request back to the parked outer activation before it resumes.
		propagateCloseInterrupt := in.isLogicallyClosed()
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
		if propagateCloseInterrupt {
			coreruntime.RequestInterrupt(outerTrap)
		}
	}, nil
}
