package wago

import (
	goruntime "runtime"
	"sync"
	"unsafe"
)

// Dedicated typed entry has no Caller scope, cross-instance namespace or
// prepared migration flag. Keep only the live root and native lease state;
// resource publication still transfers ownership before any guest continuation.
type boundedTypedHostActivation struct {
	root          *Instance
	ctrl          uintptr
	state         *instancePluginState
	entryNativeMu *sync.Mutex
}

func (a *boundedTypedHostActivation) localNativeMu() *sync.Mutex {
	if a.entryNativeMu != nil && (a.root.threadedMemoryZero || a.root.usesIndependentExecution()) {
		return a.entryNativeMu
	}
	return nil
}

func (a *boundedTypedHostActivation) dispatchI32(a0, a1 uint64) uint64 {
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchShared(a0, a1)
	}
	lease := parkedIndependentHostLease{mu: mu, version: a.state.nativeContextVersion.Load(), ctrl: a.ctrl}
	mu.Unlock()
	defer lease.resumeBoundedTyped(a)
	return I32(a.root.syncHosts[0].typedI32(AsI32(a0)))
}

func (a *boundedTypedHostActivation) dispatchI32x2(a0, a1 uint64) uint64 {
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchShared(a0, a1)
	}
	lease := parkedIndependentHostLease{mu: mu, version: a.state.nativeContextVersion.Load(), ctrl: a.ctrl}
	mu.Unlock()
	defer lease.resumeBoundedTyped(a)
	return I32(a.root.syncHosts[0].typedI32x2(AsI32(a0), AsI32(a1)))
}

// Revoked/shared dispatch retains all general signature/domain checks.
func (a *boundedTypedHostActivation) dispatchShared(a0, a1 uint64) uint64 {
	full := hostLoopActivation{root: a.root, ctrl: a.ctrl, state: a.state, entryNativeMu: a.entryNativeMu, parkedNativeContextReusable: true}
	return full.dispatchSingleTypedScalarFixedPortal(a0, a1)
}

func (l parkedIndependentHostLease) resumeBoundedTyped(a *boundedTypedHostActivation) {
	migrated := false
	if goruntime.GOARCH == "arm64" {
		l.mu.Lock()
		if !a.root.threadedMemoryZero && !a.root.usesIndependentExecution() {
			l.resumeBoundedTypedMigrated(a)
			return
		}
	} else {
		migrated = reacquireRootNative(a.root, l.mu)
	}
	if migrated || l.version == ^uint64(0) || a.state.nativeContextVersion.Load() != l.version {
		a.root.restoreTypedScalarNativeContext(l.ctrl)
	}
}

func (l parkedIndependentHostLease) resumeBoundedTypedMigrated(a *boundedTypedHostActivation) {
	l.mu.Unlock()
	nativeExecutionMu.Lock()
	nativeExecutionEpoch++
	a.root.restoreTypedScalarNativeContext(l.ctrl)
}

// HostCall's fixed view also carries no Caller capability. Its borrowed slot
// slices remain owned by the engine bridge for this synchronous callback.
func (a *boundedTypedHostActivation) dispatchHostCallView(args, results []uint64) {
	mu := a.localNativeMu()
	if mu == nil {
		a.dispatchSharedHostCallView(args, results)
		return
	}
	lease := parkedIndependentHostLease{mu: mu, version: a.state.nativeContextVersion.Load(), ctrl: a.ctrl}
	mu.Unlock()
	defer lease.resumeBoundedTyped(a)
	binding := &a.root.syncHosts[0]
	binding.fn.(HostCallFunc)(HostCall{params: compactHostSlots(args), results: compactHostSlots(results), sig: binding.sig, exact: binding.exact})
}

func (a *boundedTypedHostActivation) dispatchSharedHostCallView(args, results []uint64) {
	full := hostLoopActivation{root: a.root, ctrl: a.ctrl, state: a.state, entryNativeMu: a.entryNativeMu, parkedNativeContextReusable: true}
	full.dispatchSingleHostCallFixedView(args, results)
}

// Static-target equivalents keep the same lease and panic contract as the
// method-value fallbacks above. Their context remains a Go pointer throughout
// the bridge, so stack copying and GC can update and trace the activation.
func boundedTypedHostDispatchI32(context unsafe.Pointer, a0, a1 uint64) uint64 {
	a := (*boundedTypedHostActivation)(context)
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchShared(a0, a1)
	}
	lease := parkedIndependentHostLease{mu: mu, version: a.state.nativeContextVersion.Load(), ctrl: a.ctrl}
	mu.Unlock()
	defer lease.resumeBoundedTyped(a)
	return I32(a.root.syncHosts[0].typedI32(AsI32(a0)))
}

func boundedTypedHostDispatchI32x2(context unsafe.Pointer, a0, a1 uint64) uint64 {
	a := (*boundedTypedHostActivation)(context)
	mu := a.localNativeMu()
	if mu == nil {
		return a.dispatchShared(a0, a1)
	}
	lease := parkedIndependentHostLease{mu: mu, version: a.state.nativeContextVersion.Load(), ctrl: a.ctrl}
	mu.Unlock()
	defer lease.resumeBoundedTyped(a)
	return I32(a.root.syncHosts[0].typedI32x2(AsI32(a0), AsI32(a1)))
}

// Static target keeps the same lease and callback lifetime contract.
func boundedHostDispatchHostCallView(context unsafe.Pointer, args, results []uint64) {
	a := (*boundedTypedHostActivation)(context)
	mu := a.localNativeMu()
	if mu == nil {
		a.dispatchSharedHostCallView(args, results)
		return
	}
	lease := parkedIndependentHostLease{mu: mu, version: a.state.nativeContextVersion.Load(), ctrl: a.ctrl}
	mu.Unlock()
	defer lease.resumeBoundedTyped(a)
	binding := &a.root.syncHosts[0]
	binding.fn.(HostCallFunc)(HostCall{params: compactHostSlots(args), results: compactHostSlots(results), sig: binding.sig, exact: binding.exact})
}
