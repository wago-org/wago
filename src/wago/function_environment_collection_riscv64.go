//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func compiledManagedFunctionReachability(c *Compiled) bool { return c.usesManagedFunctionStorage() }

// Explicit collection through a numeric module can span a Runtime's managed
// edges. This private captured lease has no requesting-instance refresh: its
// topology read lock keeps the exact ordered domain list fixed until unlock.
func (in *Instance) lockManagedFunctionCollectionInvocation(id invocationID) gcInvocationLease {
	if in == nil || in.refStore == nil {
		return gcInvocationLease{}
	}
	if _, ok := managedFunctionGraphs.Load(in.refStore); !ok {
		return in.lockGCInvocation(id)
	}
	topology := in.refStore.ensureGCTopology()
	topology.RLock()
	in.refStore.mu.Lock()
	var set gcInvocationDomainSet
	for _, entry := range in.refStore.instances {
		if entry != nil && !entry.resourcesReleased {
			set.add(entry.gcDomain)
		}
	}
	compactExternAddClaimedDomainsLocked(in.refStore, &set)
	in.refStore.mu.Unlock()
	set.sort()
	domains := gcInvocationDomainView{set: &set}
	anyBorrowed, allBorrowed := false, true
	for i := 0; i < domains.len(); i++ {
		domain := domains.at(i)
		domain.invocationState.Lock()
		borrowed := domain.invocationOwner == id
		domain.invocationState.Unlock()
		anyBorrowed = anyBorrowed || borrowed
		allBorrowed = allBorrowed && borrowed
	}
	if anyBorrowed {
		topology.RUnlock()
		if !allBorrowed {
			return in.lockGCInvocation(id)
		}
		return gcInvocationLease{domains: domains, owner: id, dynamic: true}
	}
	domains.lock()
	domains.claim(id)
	return gcInvocationLease{topology: topology, domains: domains, owner: id, acquired: true, dynamic: true}
}

// collectManagedFunctionGraph runs only at an explicit full-collection boundary.
// A helper's incremental scheduling uses the same cursor separately; this full
// operation intentionally completes the fixed point before reclaiming a domain.
func collectManagedFunctionGraph(in *Instance, public *gcPublicState, topologyLeased bool) (bool, error) {
	if managedFunctionMemberCount.Load() == 0 || !topologyLeased {
		return false, nil
	}
	value, ok := managedFunctionGraphs.Load(in.refStore)
	if !ok {
		return false, nil
	}
	graph := value.(*managedFunctionGraph)
	in.refStore.mu.Lock()
	var domains gcInvocationDomainSet
	owners := make([]*Instance, 0, len(in.refStore.instances))
	for owner, entry := range in.refStore.instances {
		if entry == nil || entry.resourcesReleased {
			continue
		}
		owners = append(owners, owner)
		domains.add(entry.gcDomain)
	}
	current := in.refStore.instances[in]
	var id invocationID
	if current != nil && current.gcDomain != nil {
		current.gcDomain.invocationState.Lock()
		id = current.gcDomain.invocationOwner
		current.gcDomain.invocationState.Unlock()
	}
	compactExternAddClaimedDomainsLocked(in.refStore, &domains)
	in.refStore.mu.Unlock()
	domains.sort()
	view := gcInvocationDomainView{set: &domains}
	// A borrowed partial invocation cannot upgrade its order inside collection.
	// Keep its established conservative local roots until an outer full lease.
	if id == 0 || !view.ownedBy(id) {
		return false, nil
	}
	for i := 0; i < view.len(); i++ {
		view.at(i).mu.Lock()
	}
	defer func() {
		for i := view.len() - 1; i >= 0; i-- {
			view.at(i).mu.Unlock()
		}
	}()
	byCollector := make(map[*gc.Collector]*gcStoreDomain, view.len())
	for i := 0; i < view.len(); i++ {
		domain := view.at(i)
		byCollector[domain.collector] = domain
		domain.collector.CancelNativeAllocationBatch()
	}
	for _, owner := range owners {
		if owner.gc == nil {
			continue
		}
		domain := byCollector[owner.gc]
		if domain == nil {
			return true, fmt.Errorf("joint owner collector has no ordered domain lease")
		}
		state := owner.managedFunctionCollector(domain)
		p := owner.existingPublicGCState()
		if p != nil {
			p.mu.Lock()
		}
		err := state.bindOwnerSlots(owner)
		if err == nil && !managedFunctionOwnerClaimed(owner) {
			err = owner.syncGenericGCGlobalRootsLocked(p)
		}
		if p != nil {
			p.mu.Unlock()
		}
		if err != nil {
			return true, err
		}
	}
	graph.mu.Lock()
	for collector := range graph.collectors {
		if byCollector[collector] == nil {
			graph.mu.Unlock()
			return false, nil
		}
	}
	graph.mu.Unlock()
	mark := graph.newMark()
	groups := make(map[*gc.Collector]gc.RootGroups, len(mark.domains))
	for _, owner := range owners {
		if owner.gc == nil {
			continue
		}
		p := owner.existingPublicGCState()
		if p == nil {
			continue
		}
		roots := owner.gcCollectFrameRoots(p)
		roots.domainHostRoots = false // each physical owner is represented exactly once
		groups[owner.gc] = append(groups[owner.gc], gc.RootGroup{Class: gc.RootNativeFrame, Roots: managedFunctionTransientRoots{roots: &roots}})
	}
	for _, domain := range mark.domains {
		if err := mark.seedRoots(domain.state.collector, groups[domain.state.collector]); err != nil {
			return true, err
		}
	}
	for {
		_, done, err := mark.step(256)
		if err != nil {
			return true, err
		}
		if done {
			break
		}
	}
	return true, mark.collect()
}

func managedFunctionOwnerClaimed(owner *Instance) bool {
	if value, ok := managedFunctionMembers.Load(owner); ok {
		return value.(*managedFunctionMember).collecting.Load()
	}
	return false
}
