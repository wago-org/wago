package wago

import (
	"fmt"
	"sync/atomic"

	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/runtime"
)

// PreparedSession reserves one instance for repeated calls to a prepared
// function. Holding the reservation removes per-call lifecycle and invocation
// gate traffic. Copies share one close state and cannot duplicate the lease.
// The session and its instance are not safe for concurrent use; Close must be
// called before the instance is closed.
type PreparedSession struct {
	state *preparedSessionState
}

type preparedSessionState struct {
	fn             *WasmFunc
	lease          preparedInvocationLease
	fast           bool
	host           bool
	guardCalls     bool
	entry          executionLease
	hostCall       *runtime.PreparedHostScalarCall
	hostFixed      runtime.FixedScalarHostCall
	hostActivation hostLoopActivation
	hostGate       preparedHostLeaseGate
	active         atomic.Bool
	closeRequested atomic.Bool
	closed         atomic.Bool
}

type preparedHostLeaseGate struct {
	active   *atomic.Bool
	migrated atomic.Bool
}

// OpenSession acquires an instance reservation for repeated calls. Numeric
// signatures use the specialized scalar paths; other signatures retain the
// prepared general dispatcher while sharing the same reservation.
func (fn *WasmFunc) OpenSession() (*PreparedSession, error) {
	if fn == nil || fn.in == nil {
		return nil, fmt.Errorf("wago: open prepared session: closed prepared function")
	}
	in := fn.in
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("wago: open prepared session: %w", err)
	}
	state := &preparedSessionState{fn: fn}
	s := &PreparedSession{state: state}
	if fn.directIntFast && fn.directIsolated && in.tryPreparedDirect() {
		state.fast = true
		return s, nil
	}
	// Reserve the instance invocation identity, but acquire any shared WasmGC
	// domains only while a call is active. An idle session must not indefinitely
	// block collection or calls in another instance that shares those domains.
	state.lease = in.lockPreparedSessionInvocation()
	state.guardCalls = in.syncMode || len(in.hostLog) != 0
	// Synchronous host callbacks still park the independent native lease while
	// arbitrary Go runs. Reserving it across outer calls only removes repeated
	// context binding; callback re-entry and host-side access retain the normal
	// unlock/reacquire protocol.
	flags := in.executionFlags.Load()
	noGCDomains := in.gc == nil && flags&(executionFlagImportedGCDomain|executionFlagDynamicGCDomain|executionFlagStoreOwnedGCCollector) == 0
	if fn.scalarFast && in.syncMode && noGCDomains && in.usesIndependentExecution() && in.hasSingleDirectTypedScalarHost() {
		pluginState := in.ensurePluginState()
		pluginState.nativeShareMu.Lock()
		if !in.usesIndependentExecution() {
			pluginState.nativeShareMu.Unlock()
			return s, nil
		}
		entry, err := in.beginNativeEntry()
		if err == nil {
			rawSlots, ok := in.syncHosts[0].typedScalarSlots()
			if !ok {
				entry.unlockExecution()
				pluginState.nativeShareMu.Unlock()
				state.lease.unlock()
				in.endInvocation()
				return nil, fmt.Errorf("wago: open prepared session host entry: invalid fixed scalar signature")
			}
			prepared, prepareErr := in.eng.PrepareHostScalarFixedCall(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm, in.trap, in.results, in.ctrl, rawSlots)
			if prepareErr != nil {
				entry.unlockExecution()
				pluginState.nativeShareMu.Unlock()
				state.lease.unlock()
				in.endInvocation()
				return nil, fmt.Errorf("wago: open prepared session host entry: %w", prepareErr)
			}
			state.host = true
			state.entry = entry
			state.hostCall = prepared
			if in.hostCall == nil {
				in.hostCall = in.newHostDispatch()
			}
			state.hostActivation = hostLoopActivation{
				root:                        in,
				ctrl:                        offHeapSlicePtr(in.ctrl),
				state:                       pluginState,
				entryNativeMu:               entry.local,
				preparedMigration:           &state.hostGate.migrated,
				parkedNativeContextReusable: in.gc == nil && !in.c.threadedMemory0(),
			}
			if preparedHostFixedEnabled {
				switch in.syncHosts[0].scalarKind {
				case syncHostTypedI32:
					state.hostFixed = state.hostActivation.dispatchSingleTypedI32FixedPortal
				case syncHostTypedI32x2:
					state.hostFixed = state.hostActivation.dispatchSingleTypedI32x2FixedPortal
				}
			}
			state.hostGate.active = &state.active
			pluginState.preparedHostGate = &state.hostGate
			pluginState.nativeShareMu.Unlock()
			return s, nil
		}
		pluginState.nativeShareMu.Unlock()
	}
	return s, nil
}

// Close releases the session reservation. It is safe to call more than once.
func (s *PreparedSession) Close() {
	if s == nil || s.state == nil {
		return
	}
	state := s.state
	// A host callback may close the session that is currently invoking it. The
	// native activation has temporarily parked its lease at that point, so defer
	// destruction until the outer call has restored and left native execution.
	if state.guardCalls && state.active.Load() {
		state.closeRequested.Store(true)
		return
	}
	state.closeNow()
}

func (state *preparedSessionState) closeNow() {
	if !state.closed.CompareAndSwap(false, true) {
		return
	}
	fn := state.fn
	if state.fast {
		fn.in.ensurePluginState().invokeMu.Unlock()
	} else {
		if state.host {
			state.dropHostLease(false)
		}
		state.lease.unlock()
	}
	fn.in.endInvocation()
}

// Invoke calls the reserved prepared function. Returned results have the same
// instance-owned lifetime as WasmFunc.Invoke results.
func (s *PreparedSession) Invoke(args ...uint64) ([]uint64, error) {
	if s != nil && s.state != nil && !s.state.closed.Load() {
		switch len(args) {
		case 0:
			return s.invokeFixed(0, 0, 0, 0, 0)
		case 1:
			return s.invokeFixed(1, args[0], 0, 0, 0)
		case 2:
			return s.invokeFixed(2, args[0], args[1], 0, 0)
		case 3:
			return s.invokeFixed(3, args[0], args[1], args[2], 0)
		case 4:
			return s.invokeFixed(4, args[0], args[1], args[2], args[3])
		}
	}
	return s.invokeArgs(args)
}

// Invoke0 calls the reserved function with no argument slots.
func (s *PreparedSession) Invoke0() ([]uint64, error) { return s.invokeFixed(0, 0, 0, 0, 0) }

// Invoke1 calls the reserved function with one argument slot.
func (s *PreparedSession) Invoke1(a0 uint64) ([]uint64, error) { return s.invokeFixed(1, a0, 0, 0, 0) }

// Invoke2 calls the reserved function with two argument slots.
func (s *PreparedSession) Invoke2(a0, a1 uint64) ([]uint64, error) {
	return s.invokeFixed(2, a0, a1, 0, 0)
}

// Invoke3 calls the reserved function with three argument slots.
func (s *PreparedSession) Invoke3(a0, a1, a2 uint64) ([]uint64, error) {
	return s.invokeFixed(3, a0, a1, a2, 0)
}

// Invoke4 calls the reserved function with four argument slots.
func (s *PreparedSession) Invoke4(a0, a1, a2, a3 uint64) ([]uint64, error) {
	return s.invokeFixed(4, a0, a1, a2, a3)
}

func (s *PreparedSession) invokeFixed(count int, a0, a1, a2, a3 uint64) ([]uint64, error) {
	if s == nil || s.state == nil || s.state.closed.Load() {
		return nil, fmt.Errorf("wago: invoke closed prepared session")
	}
	state := s.state
	fn := state.fn
	if count != fn.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", fn.export, fn.paramSlots, count)
	}
	if state.fast {
		return fn.invokeDirectIntSession(a0, a1, a2, a3)
	}
	gcLease, err := state.beginCall()
	if err != nil {
		return nil, err
	}
	defer state.endCall(gcLease)
	args := [4]uint64{a0, a1, a2, a3}
	if state.host {
		return state.invokeScalarHostReserved(args[:count])
	}
	if fn.scalarFast {
		return fn.invokeScalarAdmitted(args[:count])
	}
	return fn.invokeGeneralAdmitted(args[:count])
}

func (s *PreparedSession) invokeArgs(args []uint64) ([]uint64, error) {
	if s == nil || s.state == nil || s.state.closed.Load() {
		return nil, fmt.Errorf("wago: invoke closed prepared session")
	}
	state := s.state
	fn := state.fn
	if len(args) != fn.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", fn.export, fn.paramSlots, len(args))
	}
	if state.fast && len(args) > 4 {
		return fn.invokeDirectIntWideSession(args)
	}
	gcLease, err := state.beginCall()
	if err != nil {
		return nil, err
	}
	defer state.endCall(gcLease)
	if state.host {
		return state.invokeScalarHostReserved(args)
	}
	if fn.scalarFast {
		return fn.invokeScalarAdmitted(args)
	}
	return fn.invokeGeneralAdmitted(args)
}

func (state *preparedSessionState) beginCall() (gcInvocationLease, error) {
	if state.guardCalls && !state.active.CompareAndSwap(false, true) {
		return gcInvocationLease{}, fmt.Errorf("wago: prepared session is already active")
	}
	in := state.fn.in
	// Cached host admission proves that the instance has no local or reachable
	// collector domain. Recheck the revocable flags on every call so resource
	// sharing falls back to the ordinary per-call GC lease before dispatch.
	if state.host {
		if state.hostLeaseValid() {
			return gcInvocationLease{}, nil
		}
		state.dropHostLease(false)
	}
	return in.lockGCInvocation(state.lease.state.invocationID), nil
}

func (state *preparedSessionState) endCall(gcLease gcInvocationLease) {
	if gcLease.acquired {
		gcLease.unlock()
	}
	if in := state.fn.in; in.importsFuncrefStorage() || in.table != nil {
		in.reconcileFuncrefRoots()
	}
	if !state.guardCalls {
		return
	}
	if state.closeRequested.Load() {
		state.closeNow()
	}
	state.active.Store(false)
	// A publisher that observed an active call must see its local lease released.
	if state.host && !state.hostLeaseValid() {
		state.dropHostLease(state.hostGate.migrated.Load())
	}
}

func (state *preparedSessionState) invokeScalarHostReserved(args []uint64) ([]uint64, error) {
	// A capability-free callback can still publish a captured resource. The
	// current parked activation restores safely; all later calls must leave the
	// cached local lease and use the shared/general path.
	defer func() {
		if state.host && !state.hostLeaseValid() {
			state.dropHostLease(state.hostGate.migrated.Load())
		}
	}()
	return state.fn.invokeScalarHostReserved(args, state.hostCall, state.hostFixed, &state.hostActivation)
}

func (state *preparedSessionState) hostLeaseValid() bool {
	in := state.fn.in
	flags := in.executionFlags.Load()
	return in.gc == nil && flags&executionFlagIndependent != 0 && flags&preparedFastBlocked == 0
}

func (state *preparedSessionState) dropHostLease(migrated bool) {
	if migrated {
		nativeExecutionMu.Unlock()
	} else {
		state.entry.unlockExecution()
	}
	pluginState := state.fn.in.ensurePluginState()
	pluginState.nativeShareMu.Lock()
	if pluginState.preparedHostGate == &state.hostGate {
		pluginState.preparedHostGate = nil
	}
	pluginState.nativeShareMu.Unlock()
	state.entry = executionLease{}
	state.host = false
	state.hostCall = nil
}
