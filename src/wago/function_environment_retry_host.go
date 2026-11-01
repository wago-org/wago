//go:build linux && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import "sync"

// store.mu and a topology reader are held. Domain unlink is the retirement
// claim; collector Close subsequently runs outside the writer lock and may
// overlap the final physical instance rows. Never infer collector ownership
// from such a row alone. Private domains remain tied to their live instance.
func managedFunctionDomainRegisteredLocked(store *referenceStore, owner *Instance, domain *gcStoreDomain) bool {
	if domain == nil {
		return owner.gc == nil
	}
	if domain.private {
		return true
	}
	return store.gcDomains != nil && store.gcDomains.domainForCollectorLocked(domain.collector) == domain
}

func tryManagedFunctionInvocation(g *invocationGate) bool {
	return g.state.CompareAndSwap(0, invocationGateHeld) ||
		g.state.CompareAndSwap(invocationGateRevoked, invocationGateRevoked|invocationGateHeld) ||
		g.tryAcquireHandoff()
}

// One driver and one coalesced wakeup per graph. The same mutex serializes the
// last empty-queue observation and a new request, so worker exit cannot lose it.
// idle closes only after the driver has returned from all physical callbacks.
type managedFunctionRetryState struct {
	mu      sync.Mutex
	running bool
	pending managedFunctionRetryWait
	idle    chan struct{}
}

func (w managedFunctionRetryWait) present() bool {
	return w.mutex != nil || w.topology != nil || w.invocation != nil
}

func (w managedFunctionRetryWait) wait() {
	switch {
	case w.mutex != nil:
		w.mutex.Lock()
		w.mutex.Unlock()
	case w.topology != nil:
		w.topology.RLock()
		w.topology.RUnlock()
	case w.invocation != nil:
		w.invocation.Lock()
		w.invocation.Unlock()
	}
}

func (g *managedFunctionGraph) queueHostRetry(wait managedFunctionRetryWait) {
	s := &g.hostRetry
	s.mu.Lock()
	if !s.pending.present() {
		s.pending = wait
	}
	if !s.running {
		s.running = true
		s.idle = make(chan struct{})
		go g.runHostRetry()
	}
	s.mu.Unlock()
}

func (g *managedFunctionGraph) runHostRetry() {
	s := &g.hostRetry
	for {
		s.mu.Lock()
		wait := s.pending
		s.pending = managedFunctionRetryWait{}
		if !wait.present() {
			s.running = false
			close(s.idle)
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		// No graph, store, topology, domain, or native lock is held here.
		wait.wait()
		// Collection rechecks all members' construction/invocation lifetimes.
		// The ordinary driver drops every collection lock before callbacks.
		g.drainAfterGate(0)
	}
}
