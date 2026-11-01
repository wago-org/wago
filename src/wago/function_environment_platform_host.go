//go:build linux && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Host backends do not yet describe raw function words in their exact frame
// maps. Keep environment slots conservative during an invocation; the joint
// owner pass makes them conditional only at a fully quiescent boundary.
func managedFunctionGateOwner(*instancePluginState) invocationID { return 0 }
func managedFunctionMemberActive(member *managedFunctionMember, _ invocationID) bool {
	owner := member.owner
	owner.lifeMu.Lock()
	active := owner.constructionActive || owner.invocationState.Load()&instanceInvocationCount != 0
	owner.lifeMu.Unlock()
	return active
}
func managedFunctionAfterLifetimeBoundary(in *Instance) {
	if in == nil || managedFunctionMemberCount.Load() == 0 {
		return
	}
	if value, ok := managedFunctionGraphs.Load(in.refStore); ok {
		graph := value.(*managedFunctionGraph)
		graph.pendingCollection.Store(true)
		graph.drainAfterGate(0)
	}
}
func compiledManagedFunctionReachability(c *Compiled) bool {
	if c == nil || !c.requiredFeatures.IsEnabled(CoreFeatureGC) {
		return false
	}
	for _, d := range c.GCTypeDescs {
		if d.Kind == gc.KindArray && functionStorage(d.Elem) {
			return true
		}
		for _, f := range d.Fields {
			if functionStorage(f.Kind) {
				return true
			}
		}
	}
	return false
}
func (c *Compiled) usesManagedOpaqueStorage() bool { return compiledManagedFunctionReachability(c) }
func functionEnvironmentHostQuiescent(g *managedFunctionGraph) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.gateStillOwnedLocked(0)
}
func (in *Instance) managedFunctionEnsureStore(*gcStoreDomain) error {
	if in.refStore == nil {
		return fmt.Errorf("managed function requires a reference store")
	}
	return nil
}
func externAggregateStorage(gc.StorageKind) bool   { return false }
func opaqueAggregateStorage(k gc.StorageKind) bool { return functionStorage(k) }
func (in *Instance) managedOpaqueTypedValue(k gc.StorageKind, want ValueTypeDescriptor, bits uint64) gc.Value {
	return in.managedFunctionTypedValue(k, want, bits)
}
func (a *managedFunctionAggregateScratch) changeOpaque(s *managedFunctionCollector, in *Instance, k gc.StorageKind, bits uint64, delta int64, allocate bool) {
	if !functionStorage(k) {
		panic(gcHelperFailuref("invalid function edge kind"))
	}
	a.change(s, in, bits, delta, allocate)
}

// Compact extern and exception ownership keep their existing host representation.
type managedFunctionExceptionScratch struct{}

func compactExternAddClaimedDomainsLocked(*referenceStore, *gcInvocationDomainSet) {}
func compactExternHostRootIsStrong(*gc.Collector, uint32) bool                     { return true }
func (*managedFunctionMarkDomain) clearUnreachableHostExternRoots() error          { return nil }

func (s *managedFunctionCollector) publishHostEnvironment(env *managedFunctionEnv) error {
	slot := s.collector.NewGlobalSlot(env.root.GetRef())
	return s.bindPersistentSlot(env.member.owner, gc.SlotGlobal, slot)
}
