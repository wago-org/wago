//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

func managedFunctionRegisteredLocked(*referenceStore, *Instance)     {}
func managedFunctionLifetimeLocked(*referenceStore, *Instance, bool) {}
func managedFunctionPrepareRegistration(*referenceStore, *Instance)  {}

func managedFunctionBindInvocationState(*Instance, *instancePluginState)      {}
func managedFunctionAfterInvocationUnlock(*instancePluginState, invocationID) {}
func managedFunctionAfterClose(*Instance)                                     {}
func drainManagedFunctionStoreReclaims(*referenceStore)                       {}

func compiledManagedFunctionReachability(*Compiled) bool { return false }
func (in *Instance) lockManagedFunctionCollectionInvocation(id invocationID) gcInvocationLease {
	return in.lockGCInvocation(id)
}
func collectManagedFunctionGraph(*Instance, *gcPublicState, bool) (bool, error) { return false, nil }

func managedFunctionOwnerRootsStrong(*Instance, *gc.Collector) bool { return true }

func visitManagedFunctionFrameRoot(_ *Instance, descriptor uint64, _ func(gc.RootSlot) bool, _ gc.RootRefSink) bool {
	if descriptor != 0 {
		panic(gcHelperFailuref("function frame roots require the managed owner runtime"))
	}
	return true
}
func visitManagedFunctionOwnerFrameRoot(*Instance, *gc.Collector, func(gc.RootSlot) bool, gc.RootRefSink) bool {
	return true
}

func (*Instance) managedFunctionAggregateMayAllocate(uint32, []uint64) bool { return false }
func (*Instance) dispatchManagedFunctionAggregate(uint32, []uint64, []uint64, gc.RootSet, *gcPublicState) bool {
	return false
}

type managedFunctionConstruction struct{}

func newManagedFunctionConstruction(*Compiled, *gc.Collector, *gc.Slots) *managedFunctionConstruction {
	return nil
}
func (*managedFunctionConstruction) roots(fallback gc.Slots) gc.RootSet          { return fallback }
func (*managedFunctionConstruction) close()                                      {}
func (*managedFunctionConstruction) bind(*Instance, *gcStoreDomain) error        { return nil }
func recordManagedFunctionConstruction(*gc.Collector, *Compiled, gc.Ref, uint32) {}

func managedFunctionDeferFinalization(*Instance) bool { return false }
