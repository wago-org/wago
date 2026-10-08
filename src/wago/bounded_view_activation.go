package wago

import "unsafe"

// Bounded root-only view entry needs the compact lease state plus Caller's
// lazy invocation cache. Prepared migration is observed through root ownership,
// and context reuse is fixed by admission, so neither needs a per-entry field.
type boundedViewHostActivation struct {
	boundedTypedHostActivation
	invocation hostInvocationContext
}

func (a *boundedViewHostActivation) context() hostInvocationContext {
	if a.invocation.id != 0 && a.ctrl == offHeapSlicePtr(a.root.ctrl) {
		return a.invocation
	}
	var invocation hostInvocationContext
	// The binding count is published before every map entry. With no bindings,
	// the current admitted root supplies exactly the ordinary map fallback.
	if activeHostInvocationBindings.Load() == 0 {
		if id := a.root.currentInvocationID(); id != 0 {
			invocation = hostInvocationContext{id: id, reservation: currentInvocationReservation(a.root)}
		}
	} else {
		invocation = activeHostInvocationContext(a.root)
	}
	if a.root != nil && a.ctrl != 0 && a.ctrl == offHeapSlicePtr(a.root.ctrl) && invocation.id != 0 {
		a.invocation = invocation
	}
	if invocation.empty() {
		invocation = activeHostInvocationContext(a.root)
	}
	return invocation
}

func (a *boundedViewHostActivation) dispatchCallerView(args, results []uint64) {
	mu := a.localNativeMu()
	if mu == nil {
		a.dispatchSharedCallerView(args, results)
		return
	}
	invocation := a.context()
	lease := parkedIndependentHostLease{mu: mu, version: a.state.nativeContextVersion.Load(), ctrl: a.ctrl}
	mu.Unlock()
	defer lease.resumeBoundedTyped(&a.boundedTypedHostActivation)
	previousID := a.state.activations.boundedID.Load()
	a.state.activations.boundedID.Store(uint64(invocation.id))
	defer a.state.activations.boundedID.Store(previousID)
	binding := &a.root.syncHosts[0]
	scope := &a.state.hostScope
	generation, parent := scope.beginGeneration(invocation.parent)
	caller := instanceHostModule{
		in: a.root, scope: scope, generation: generation, parentGeneration: parent,
		invocationID: invocation.id, reservation: invocation.reservation, exact: binding.exact,
	}
	defer scope.end(generation, parent)
	binding.fn.(CallerHostCallFunc)(Caller{instanceHostModule: caller}, HostCall{
		params: compactHostSlots(args), results: compactHostSlots(results), sig: binding.sig, exact: binding.exact,
	})
}

func (a *boundedViewHostActivation) dispatchSharedCallerView(args, results []uint64) {
	full := hostLoopActivation{
		root: a.root, ctrl: a.ctrl, state: a.state, entryNativeMu: a.entryNativeMu,
		invocation: a.invocation, parkedNativeContextReusable: true,
	}
	defer func() { a.invocation = full.invocation }()
	full.dispatch(a.ctrl, 0, args, results)
}

// Static target keeps the same lease and callback lifetime contract.
func boundedHostDispatchCallerView(context unsafe.Pointer, args, results []uint64) {
	a := (*boundedViewHostActivation)(context)
	mu := a.localNativeMu()
	if mu == nil {
		a.dispatchSharedCallerView(args, results)
		return
	}
	invocation := a.context()
	lease := parkedIndependentHostLease{mu: mu, version: a.state.nativeContextVersion.Load(), ctrl: a.ctrl}
	mu.Unlock()
	defer lease.resumeBoundedTyped(&a.boundedTypedHostActivation)
	previousID := a.state.activations.boundedID.Load()
	a.state.activations.boundedID.Store(uint64(invocation.id))
	defer a.state.activations.boundedID.Store(previousID)
	binding := &a.root.syncHosts[0]
	scope := &a.state.hostScope
	generation, parent := scope.beginGeneration(invocation.parent)
	caller := instanceHostModule{
		in: a.root, scope: scope, generation: generation, parentGeneration: parent,
		invocationID: invocation.id, reservation: invocation.reservation, exact: binding.exact,
	}
	defer scope.end(generation, parent)
	binding.fn.(CallerHostCallFunc)(Caller{instanceHostModule: caller}, HostCall{
		params: compactHostSlots(args), results: compactHostSlots(results), sig: binding.sig, exact: binding.exact,
	})
}
