package wago

import (
	"encoding/binary"
	goruntime "runtime"

	"github.com/wago-org/wago/internal/runtimebridge"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Bound only ordinary numeric Go callbacks. Owned, gated, reference and legacy
// HostModule bindings retain the generic dispatch namespace and root handling.
func boundedMultiHostShapes(bindings []syncHostBinding) []uint32 {
	if len(bindings) < 2 || len(bindings) > 64 {
		return nil
	}
	shapes := make([]uint32, len(bindings))
	for i := range bindings {
		b := &bindings[i]
		_, caller := b.fn.(CallerHostCallFunc)
		if b.sig == nil || b.gate != nil || b.scalarKind == syncHostNonScalar || !(b.hostCall || caller || b.scalarKind >= syncHostTypedI32) {
			return nil
		}
		n, nres := len(b.sig.Params), len(b.sig.Results)
		if n > coreruntime.MaxHostArity || nres > coreruntime.MaxHostArity {
			return nil
		}
		shapes[i] = uint32(n) | uint32(nres)<<16
	}
	return shapes
}

func (in *Instance) tryInvokeCachedMultiNumeric(export string, args []uint64) ([]uint64, error, bool) {
	state := in.pluginState.Load()
	if codeProfileEnabled || state == nil || state.multiHost == nil || !in.boundedHostSegments() ||
		!in.invocationState.CompareAndSwap(0, 1) {
		return nil, nil, false
	}
	if !state.invokeMu.state.CompareAndSwap(0, invocationGateHeld) {
		in.endDirectInvocation()
		return nil, nil, false
	}
	ic := in.findInvokeCache(export)
	privateRefStore := in.refStore == nil || in.refStore.private
	if ic == nil || ic.li < 0 || ic.hasFuncRefParams || ic.hasFuncRefResults || len(args) != int(ic.paramSlots) ||
		!privateRefStore || in.guestStorageBorrowed() || !in.preparedFastStateValid() || !in.usesIndependentExecution() ||
		in.gc != nil || in.threadedMemoryZero || in.table != nil || in.importsFuncrefStorage() || len(in.hostLog) != 0 {
		state.invokeMu.Unlock()
		in.endDirectInvocation()
		return nil, nil, false
	}
	state.invocationID = newInvocationID()
	defer func() {
		state.invocationID = 0
		state.invokeMu.Unlock()
		in.endDirectInvocation()
	}()
	copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, ic.slotWide[:ic.paramSlots], ic.paramWidthClass)
	entry := in.base + uintptr(in.c.Entry[ic.li])
	if err := in.callCachedBoundedMultiHost(entry, state.multiHost); err != nil {
		return nil, err, true
	}
	out := in.resultVals[:ic.resultSlots]
	copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), ic.slotWide[ic.paramSlots:], ic.resultWidthClass)
	return out, nil, true
}

func (in *Instance) callCachedBoundedMultiHost(entry uintptr, layout *boundedMultiHostLayout) (err error) {
	locked, reuse, err := in.beginCachedBoundedViewEntry()
	if err != nil {
		return err
	}
	defer in.unlockNativeEntry(locked)
	defer func() { err = in.decorateTrap(err) }()
	defer recoverNativeSyncPanic(&err)
	if activeHostInvocationBindings.Load() != 0 {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}
	state := in.ensurePluginState()
	if !reuse {
		in.jm.SetStackFence(in.eng.StackLimit())
		in.jm.SetCustomCtx(offHeapSlicePtr(in.ctrl))
		state.boundedViewVersion = state.nativeContextVersion.Load()
		state.boundedViewMemBase = in.jm.LinMemBase()
	}
	activation := hostLoopActivation{
		root: in, ctrl: offHeapSlicePtr(in.ctrl), state: state,
		entryNativeMu: locked.local, parkedNativeContextReusable: true,
	}
	err = in.eng.CallWithHostBaseMultiViewBounded(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, layout.shapes, activation.dispatchBoundedMultiHostView)
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return err
}

type boundedMultiHostLayout struct{ shapes []uint32 }

func (in *Instance) hasBoundedMultiHostView() bool {
	state := in.pluginState.Load()
	return state != nil && state.multiHost != nil && in.boundedHostSegments() && in.gc == nil && in.executionFlags.Load()&(executionFlagImportedGCDomain|executionFlagDynamicGCDomain|executionFlagStoreOwnedGCCollector) == 0
}

func (a *hostLoopActivation) dispatchBoundedMultiHostView(args, results []uint64) {
	index := binary.LittleEndian.Uint32(a.root.ctrl[coreruntime.HostCtrlImportIndexOffset:])
	mu := a.localNativeMu()
	if mu == nil {
		a.dispatch(a.ctrl, index, args, results)
		return
	}
	lease := a.parkIndependentHostCallbackWithMu(a.ctrl, mu)
	defer lease.resume(a)
	binding := &a.root.syncHosts[index]
	if binding.hostCall {
		binding.fn.(HostCallFunc)(HostCall{params: compactHostSlots(args), results: compactHostSlots(results), sig: binding.sig, exact: binding.exact})
		return
	}
	if fn, ok := binding.fn.(CallerHostCallFunc); ok {
		invocation := a.context(a.root)
		previousID := a.state.activations.boundedID.Load()
		a.state.activations.boundedID.Store(uint64(invocation.id))
		defer a.state.activations.boundedID.Store(previousID)
		scope := &a.state.hostScope
		generation, parent := scope.beginGeneration(invocation.parent)
		caller := instanceHostModule{in: a.root, scope: scope, generation: generation, parentGeneration: parent, invocationID: invocation.id, reservation: invocation.reservation, exact: binding.exact}
		defer scope.end(generation, parent)
		fn(Caller{instanceHostModule: caller}, HostCall{params: compactHostSlots(args), results: compactHostSlots(results), sig: binding.sig, exact: binding.exact})
		return
	}
	binding.callUnchecked(instanceHostModule{}, args, results)
}
