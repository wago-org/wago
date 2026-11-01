//go:build linux && !tinygo && !wago_target_tinygo

package wago

import "sync"

// Closed owners can hold only conditional function environments. Request a
// joint scan after terminal close. Host contention arranges an independent
// wakeup; Close never waits for another invocation or native execution lock.
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
	// Active/construction participants have a mandatory outer boundary. Leave
	// their work synchronous there; only otherwise-quiescent lock contention
	// needs an independent wakeup. Collection rechecks after acquiring leases.
	if !functionEnvironmentHostQuiescent(g) {
		return
	}
	if !g.collecting.TryLock() {
		g.queueHostRetry(managedFunctionRetryWait{mutex: &g.collecting})
		return
	}
	defer g.collecting.Unlock()
	topology := g.store.ensureGCTopology()
	// Respect queued writers as ordinary RLock does, but do not wait while
	// Close or an invocation-return path owns the cleanup boundary.
	topology.mu.Lock()
	if topology.writer || topology.writers != 0 {
		topology.mu.Unlock()
		g.queueHostRetry(managedFunctionRetryWait{topology: topology})
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
		if !tryManagedFunctionInvocation(&view.at(acquired).invocationMu) {
			for i := acquired - 1; i >= 0; i-- {
				view.at(i).invocationMu.Unlock()
			}
			g.queueHostRetry(managedFunctionRetryWait{invocation: &view.at(acquired).invocationMu})
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
			g.queueHostRetry(managedFunctionRetryWait{mutex: mutexes[locked]})
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

// A retry waits for only the lock that prevented progress, holding no other
// locks. It then restarts the ordinary topology -> domains -> native protocol.
type managedFunctionRetryWait struct {
	mutex      *sync.Mutex
	topology   *gcDomainTopology
	invocation *invocationGate
}
