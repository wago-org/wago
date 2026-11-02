//go:build linux && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
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
	if in == nil {
		return
	}
	// Boundary publication is lazy and protected by lifeMu. Drop it before
	// entering graph/topology collection to preserve the lock order.
	in.lifeMu.Lock()
	store := in.refStore
	in.lifeMu.Unlock()
	if value, ok := managedFunctionGraphs.Load(store); ok {
		graph := value.(*managedFunctionGraph)
		graph.pendingCollection.Store(true)
		graph.drainAfterGate(0)
	}
}
func compiledManagedFunctionReachability(c *Compiled) bool {
	if c == nil || !c.requiredFeatures.IsEnabled(CoreFeatureGC) {
		return false
	}
	// Conversion is a distinct execution product, but its aggregate helpers
	// need the same typed function-owner edges as generic GC execution. An
	// unused type declaration or nonallocating bridge does not activate them.
	convertedAggregate := c.stagedGCStructProduct().requiresExternConversion() && c.genericGCFrameRoots() != nil && len(c.genericGCFrameRoots().safepoints) != 0
	if !c.usesGenericGCExecution() && !convertedAggregate {
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

// A standalone bare provider has no descriptor arena. Its importer owns the
// proxy bytes and the installed function attachment owns the provider. Retain
// that proxy owner, not an unregistered foreign-store member. This is solely a
// physical ownership proof; public cross-store token admission is unchanged.
func managedFunctionPrivateProxyOwner(source *Instance, descriptor uint64) *Instance {
	index, ok := source.funcrefDescriptorIndex(descriptor)
	if !ok || index < 0 || index >= source.c.NumImports || index >= len(source.c.Imports) {
		return nil
	}
	export, ok := source.imports[source.c.functionImportBindingKey(index)].(*InstanceExport)
	if !ok || export == nil || export.inst == nil || source.ownsTransferredFunctionAttachment(export.inst) {
		return nil
	}
	producer := export.inst
	if producer.refStore == source.refStore || producer.gc != nil || producer.reachesPrivateGCInvocationDomain() {
		return nil
	}
	if _, exists := producer.localFuncrefDescriptor(export.localIdx); exists {
		return nil
	}
	producer.lifeMu.Lock()
	live := !producer.resourcesClosed && producer.resourceRefs > 0
	producer.lifeMu.Unlock()
	if !live {
		return nil
	}
	off := (index + 1) * coreruntime.FuncRefDescBytes
	if binary.LittleEndian.Uint64(source.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:]) != descriptor {
		return nil
	}
	return source
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
	if s.hostEnvSlots == nil {
		s.hostEnvSlots = make(map[uint32]gc.Ref)
	}
	ref := env.root.GetRef()
	var slot uint32
	if n := len(s.hostFreeSlots); n != 0 {
		slot = s.hostFreeSlots[n-1]
		if err := s.collector.SetGlobalSlot(slot, ref); err != nil {
			return err
		}
		s.hostFreeSlots = s.hostFreeSlots[:n-1]
	} else {
		slot = s.collector.NewGlobalSlot(ref)
	}
	s.hostEnvSlots[slot] = ref
	return nil
}
