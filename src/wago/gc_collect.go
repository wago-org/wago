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
// serialized with native execution and collector-domain mutation.
func (in *Instance) CollectGC() error {
	if in == nil || in.gc == nil || in.c == nil {
		return fmt.Errorf("wago: instance has no live WasmGC collector")
	}
	// Public collection is an independent operation, not a callback-scoped
	// re-entry. Use a fresh identity instead of racing the instance's active
	// invocation ID; GCHostModule.CollectGC handles lease ownership explicitly.
	invocation := in.lockGCInvocation(newInvocationID())
	defer invocation.unlock()
	return in.collectGC()
}

func (in *Instance) collectGC() error {
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
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	public.mu.Lock()
	defer public.mu.Unlock()
	if err := in.syncGenericGCGlobalRootsLocked(public); err != nil {
		return err
	}
	public.frameRoots = in.gcCollectFrameRoots(public)
	return in.gc.CollectFull(&public.frameRoots)
}
