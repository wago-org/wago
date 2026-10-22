//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

// Gate registration is cold and does not root any additional native resource.
// The graph already holds Go identities until their physical lifetime retires.
var managedFunctionInvocationStates managedFunctionPointerRegistry[*instancePluginState] // *instancePluginState -> *managedFunctionGraph

func managedFunctionBindInvocationState(in *Instance, state *instancePluginState) {
	if managedFunctionMemberCount.Load() == 0 || in == nil || state == nil {
		return
	}
	if value, ok := managedFunctionMembers.Load(in); ok {
		managedFunctionInvocationStates.Store(state, value.(*managedFunctionMember).graph)
	}
}

func managedFunctionAfterInvocationUnlock(state *instancePluginState, id invocationID) {
	if managedFunctionMemberCount.Load() == 0 {
		return
	}
	if value, ok := managedFunctionInvocationStates.Load(state); ok {
		value.(*managedFunctionGraph).drainAfterGate(id)
	}
}

func drainManagedFunctionStoreReclaims(store *referenceStore) {
	if managedFunctionMemberCount.Load() == 0 || store == nil {
		return
	}
	if value, ok := managedFunctionGraphs.Load(store); ok {
		value.(*managedFunctionGraph).drainAfterGate(0)
	}
}

// graph.mu is held. A zero ID is used outside a proven invocation-chain return
// and conservatively waits for every active gate. Same-chain nested calls defer
// to the final outer gate, while unrelated chains need not stop all reclamation.
func (g *managedFunctionGraph) gateStillOwnedLocked(id invocationID) bool {
	for _, member := range g.rows {
		if member == nil {
			continue
		}
		state := member.owner.pluginState.Load()
		if state == nil {
			continue
		}
		state.activations.mu.Lock()
		owner := state.railshotInvocation.gateOwner
		state.activations.mu.Unlock()
		if owner != 0 && (id == 0 || owner == id) {
			return true
		}
	}
	return false
}

func (g *managedFunctionGraph) drainAfterGate(id invocationID) {
	if g.pendingCollection.Load() {
		g.tryCollectAfterGate()
	}
	if g.pendingReclaims.Load() == 0 && g.pendingFinalizers.Load() == 0 && !g.pendingMetadata.Load() {
		return
	}
	g.mu.Lock()
	if g.drainingReclaims || g.gateStillOwnedLocked(id) {
		g.mu.Unlock()
		return
	}
	g.drainingReclaims = true
	g.mu.Unlock()
	var cleanup resourceCleanup
	for {
		g.pendingMetadata.Store(false)
		// A native pin can be the last reason a public converted token stayed
		// live during Runtime close. This driver owns no invocation/native lock;
		// release its root only after the last pin and closed-host proof agree.
		g.store.mu.Lock()
		publicTokens := g.store.closedHostPublicTokenEntriesLocked()
		g.store.mu.Unlock()
		cleanup.run(func() { releaseReferenceEntries(publicTokens) })
		g.mu.Lock()
		collectors := make([]*managedFunctionCollector, 0, len(g.collectors))
		for _, state := range g.collectors {
			collectors = append(collectors, state)
		}
		g.mu.Unlock()
		for _, state := range collectors {
			cleanup.run(func() { drainExceptionReclaims(state.collector) })
			cleanup.run(func() { drainManagedFunctionReclaims(state.collector) })
		}
		g.mu.Lock()
		var finalizers []*Instance
		for _, member := range g.rows {
			if member != nil && member.finalizationPending {
				member.finalizationPending = false
				g.pendingFinalizers.Add(^uint64(0))
				finalizers = append(finalizers, member.owner)
			}
		}
		g.mu.Unlock()
		for _, owner := range finalizers {
			cleanup.run(owner.tryFinalize)
		}

		// A reentrant callback can queue another journal, but cannot recursively
		// own this driver. Consume that work before relinquishing the wakeup claim.
		g.mu.Lock()
		again := (g.pendingReclaims.Load() != 0 || g.pendingFinalizers.Load() != 0 || g.pendingMetadata.Load()) && !g.gateStillOwnedLocked(id)
		if !again {
			g.drainingReclaims = false
		}
		g.mu.Unlock()
		if !again {
			break
		}
	}
	cleanup.finish()
}

// Native target scopes release before their outer collector invocation lease.
// A closed producer can therefore be quiescent while the caller still owns a
// topology reader. Defer its finalization (not the root-count decrement) to the
// same synchronous outer cleanup boundary as Env physical reclamation.
func managedFunctionDeferFinalization(in *Instance) bool {
	if managedFunctionMemberCount.Load() == 0 || in == nil || in.invocationState.Load()&instanceInvocationClosed == 0 {
		return false
	}
	value, ok := managedFunctionMembers.Load(in)
	if !ok {
		return false
	}
	member := value.(*managedFunctionMember)
	g := member.graph
	g.store.mu.Lock()
	topology := g.store.gcDomains
	g.store.mu.Unlock()
	if topology == nil {
		return false
	}
	topology.mu.Lock()
	readers := topology.readers != 0
	topology.mu.Unlock()
	if !readers {
		return false
	}
	g.mu.Lock()
	if g.retired || g.members[in] != member {
		g.mu.Unlock()
		return false
	}
	if !member.finalizationPending {
		member.finalizationPending = true
		g.pendingFinalizers.Add(1)
	}
	g.mu.Unlock()
	return true
}
