package wago

import (
	"fmt"
	"sync"
)

func gcCollectFrameRoots(in *Instance, public *gcPublicState, frameLayout uint8, allowExternalReturn bool) gcNativeFrameRoots {
	return gcNativeFrameRoots{
		owner:               in,
		frameLayout:         frameLayout,
		allowExternalReturn: allowExternalReturn,
		suspended:           public,
	}
}

// CollectGC performs one full collection for this instance's exact Runtime GC
// domain. It is safe to call from a synchronous host import: parked native
// activations, globals, tables, retained public references, and same-domain
// instances remain rooted for the duration of the collection.
//
// Instances without a live WasmGC collector return an error. Collection remains
// serialized with native execution and collector-domain mutation, and fails with
// ErrPermissionDenied while callback-scoped guest storage is borrowed.
func (in *Instance) CollectGC() error {
	if err := in.beginInvocation(); err != nil {
		return err
	}
	defer in.endInvocation()
	defer func() { drainManagedFunctionStoreReclaims(in.refStore) }()
	if in == nil || in.gc == nil || in.c == nil {
		return fmt.Errorf("wago: instance has no live WasmGC collector")
	}
	if in.guestStorageBorrowed() {
		return fmt.Errorf("wago: collection is unavailable while guest storage is borrowed: %w", ErrPermissionDenied)
	}
	in.revokePrivateGCEntry()
	// Public collection is an independent operation, not a callback-scoped
	// re-entry. Use a fresh identity instead of racing the instance's active
	// invocation ID; GCHostModule.CollectGC handles lease ownership explicitly.
	invocation := in.lockManagedFunctionCollectionInvocation(newInvocationID())
	defer invocation.unlock()
	return in.collectGCWithJoint(invocation.dynamic)
}

func (in *Instance) collectGC() error {
	return in.collectGCWithJoint(in.executionFlags.Load()&executionFlagDynamicGCDomain != 0)
}

func (in *Instance) collectGCWithJoint(topologyLeased bool) error {
	public := in.existingPublicGCState()
	if public == nil {
		return errGenericGCRootState
	}
	var nativeMu *sync.Mutex
	if in.usesIndependentExecution() {
		nativeMu = in.independentNativeExecutionMu()
	} else if in.c.threadedMemory0() {
		nativeMu = &in.memoryDir.nativeMu
	} else {
		nativeMu = &nativeExecutionMu
	}
	nativeMu.Lock()
	defer nativeMu.Unlock()
	defer drainExceptionReclaims(in.gc)
	if handled, err := collectManagedFunctionGraph(in, public, topologyLeased); handled {
		return err
	}
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	public.mu.Lock()
	defer public.mu.Unlock()
	if err := in.prepareGCCollectionRootsLocked(public); err != nil {
		return err
	}
	return in.gc.CollectFull(&public.frameRoots)
}

// prepareGCCollectionRootsLocked prepares the standard root universe while
// native execution, the collector domain, and public.mu are held.
func (in *Instance) prepareGCCollectionRootsLocked(public *gcPublicState) error {
	if err := in.syncGenericGCGlobalRootsLocked(public); err != nil {
		return err
	}
	public.frameRoots = in.gcCollectFrameRoots(public)
	return nil
}
