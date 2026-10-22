//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "sync"

// Closed owners can hold only conditional function environments. Request a
// joint scan after terminal close, then leave contention to the next outer call
// boundary. Close never waits for another invocation or native execution lock.
func managedFunctionAfterClose(in *Instance) {
	if managedFunctionMemberCount.Load() == 0 || in == nil {
		return
	}
	if value, ok := managedFunctionGraphs.Load(in.refStore); ok {
		graph := value.(*managedFunctionGraph)
		graph.pendingCollection.Store(true)
		graph.drainAfterGate(0)
	}
}

func (g *managedFunctionGraph) tryCollectAfterGate() {
	if !g.collecting.CompareAndSwap(false, true) {
		return
	}
	defer g.collecting.Store(false)
	topology := g.store.ensureGCTopology()
	// Respect queued writers as ordinary RLock does, but do not wait while
	// Close or an invocation-return path owns the cleanup boundary.
	topology.mu.Lock()
	if topology.writer || topology.writers != 0 {
		topology.mu.Unlock()
		return
	}
	topology.readers++
	topology.mu.Unlock()
	defer topology.RUnlock()
	g.store.mu.Lock()
	var domains gcInvocationDomainSet
	var requester *Instance
	mutexes := make([]*sync.Mutex, 0, len(g.store.instances)+1)
	mutexes = append(mutexes, &nativeExecutionMu)
	for owner, entry := range g.store.instances {
		if entry == nil || entry.resourcesReleased {
			continue
		}
		domains.add(entry.gcDomain)
		if owner.gc != nil {
			requester = owner
		}
		var mu *sync.Mutex
		if owner.usesIndependentExecution() {
			mu = owner.independentNativeExecutionMu()
		} else if owner.threadedMemoryZero && owner.memoryDir != nil {
			mu = &owner.memoryDir.nativeMu
		}
		if mu != nil {
			found := false
			for _, previous := range mutexes {
				found = found || previous == mu
			}
			if !found {
				mutexes = append(mutexes, mu)
			}
		}
	}
	compactExternAddClaimedDomainsLocked(g.store, &domains)
	g.store.mu.Unlock()
	if requester == nil {
		g.pendingCollection.Store(false)
		return
	}
	domains.sort()
	view := gcInvocationDomainView{set: &domains}
	acquired := 0
	for ; acquired < view.len(); acquired++ {
		if !view.at(acquired).invocationMu.state.CompareAndSwap(0, invocationGateHeld) {
			for i := acquired - 1; i >= 0; i-- {
				view.at(i).invocationMu.Unlock()
			}
			return
		}
	}
	id := newInvocationID()
	view.claim(id)
	defer func() { view.release(id); view.unlock() }()
	locked := 0
	for ; locked < len(mutexes); locked++ {
		if !mutexes[locked].TryLock() {
			for i := locked - 1; i >= 0; i-- {
				mutexes[i].Unlock()
			}
			return
		}
	}
	defer func() {
		for i := len(mutexes) - 1; i >= 0; i-- {
			mutexes[i].Unlock()
		}
	}()
	// Consume only this request. A concurrent Close publishes a new request
	// while this scan runs, and its later boundary can retry that work.
	g.pendingCollection.Store(false)
	if handled, err := collectManagedFunctionGraph(requester, requester.existingPublicGCState(), true); !handled || err != nil {
		g.pendingCollection.Store(true)
	}
}
