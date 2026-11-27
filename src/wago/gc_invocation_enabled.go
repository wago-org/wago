// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_reference_store && !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import "context"

type gcInvocationLease struct {
	in       *Instance
	topology *gcDomainTopology
	domains  gcInvocationDomainView
	owner    invocationID
	acquired bool
	dynamic  bool
}

func (l gcInvocationLease) isAcquired() bool { return l.acquired }

func (l gcInvocationLease) isDynamic() bool { return l.dynamic }

func (l gcInvocationLease) leasedTopology() *gcDomainTopology { return l.topology }

func (l gcInvocationLease) release(cleanup *resourceCleanup) {
	cleanup.run(l.unlock)
}

func (in *Instance) lockGCInvocation(owner invocationID) gcInvocationLease {
	// Ordinary scalar calls still acquire the invocation gate, but modules with
	// no collector or imported/dynamic GC domain have nothing to lock here.
	if !in.mayNeedGCInvocationLease() {
		return gcInvocationLease{}
	}
	lease, _ := in.lockGCInvocationContext(context.Background(), owner)
	return lease
}

func (in *Instance) lockGCInvocationContext(ctx context.Context, owner invocationID) (gcInvocationLease, error) {
	if ctx != nil && ctx.Done() == nil {
		ctx = nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return gcInvocationLease{}, err
		}
	}
	if owner == 0 {
		owner = newInvocationID()
	}
	dynamic := in != nil && in.refStore != nil && in.executionFlags.Load()&executionFlagDynamicGCDomain != 0
	var topology *gcDomainTopology
	if dynamic {
		in.refStore.mu.Lock()
		topology = in.refStore.gcTopologyLocked()
		in.refStore.mu.Unlock()
		if topology == nil {
			panic("wago: dynamic Runtime GC invocation has no topology")
		}
		if err := topology.gate.lockContext(ctx, false); err != nil {
			return gcInvocationLease{}, err
		}
	}
	domains := in.gcInvocationDomains()
	if domains.len() == 0 {
		if dynamic {
			return gcInvocationLease{in: in, topology: topology, owner: owner, acquired: true, dynamic: true}, nil
		}
		return gcInvocationLease{}, nil
	}
	// A native cross-instance call reuses the public root's invocation identity.
	// The root pre-acquires the transitive, globally ordered domain set, so a
	// target reached through its retained imports borrows the already-held lease
	// instead of recursively locking the same non-reentrant mutexes.
	first := domains.at(0)
	first.invocationState.Lock()
	borrowed := first.invocationOwner == owner
	first.invocationState.Unlock()
	if borrowed {
		if !domains.ownedBy(owner) {
			panic("wago: partial Runtime GC invocation lease ownership")
		}
		if dynamic {
			topology.gate.RUnlock()
		}
		return gcInvocationLease{in: in, topology: topology, domains: domains, owner: owner}, nil
	}
	if ctx == nil {
		domains.lock()
	} else if err := domains.lockContext(ctx); err != nil {
		if dynamic {
			topology.gate.RUnlock()
		}
		return gcInvocationLease{}, err
	}
	domains.claim(owner)
	return gcInvocationLease{in: in, topology: topology, domains: domains, owner: owner, acquired: true, dynamic: dynamic}, nil
}

func (l gcInvocationLease) unlock() {
	if !l.acquired {
		return
	}
	domains := l.domains
	if l.dynamic && l.in != nil {
		domains = l.in.gcInvocationDomains()
	}
	domains.release(l.owner)
	domains.unlock()
	if l.dynamic {
		l.topology.gate.RUnlock()
	}
}

func (in *Instance) ownsGCInvocation(owner invocationID) bool {
	domain := in.gcInvocationDomain()
	if domain == nil || owner == 0 {
		return false
	}
	domain.invocationState.Lock()
	owned := domain.invocationOwner == owner
	domain.invocationState.Unlock()
	return owned
}

// A scalar cross-instance relay can own imported domains on behalf of the
// producer whose frame is parked. Suspend the public root's aggregate lease.
func hostGCLeaseOwner(active, root *Instance) *Instance {
	if root != nil && root.executionFlags.Load()&executionFlagImportedGCDomain != 0 {
		return root
	}
	return active
}

type gcInvocationSuspension struct {
	in       *Instance
	topology *gcDomainTopology
	domains  gcInvocationDomainView
	owner    invocationID
	dynamic  bool
	active   bool
}

func (in *Instance) suspendGCInvocation(owner invocationID) gcInvocationSuspension {
	dynamic := in != nil && in.refStore != nil && in.executionFlags.Load()&executionFlagDynamicGCDomain != 0
	var topology *gcDomainTopology
	if dynamic {
		in.refStore.mu.Lock()
		topology = in.refStore.gcTopologyLocked()
		in.refStore.mu.Unlock()
		if topology == nil {
			panic("wago: dynamic Runtime GC invocation has no topology")
		}
	}
	domains := in.gcInvocationDomains()
	if owner == 0 || domains.len() == 0 && !dynamic {
		return gcInvocationSuspension{}
	}
	if !domains.ownedBy(owner) {
		if !domains.dynamic && domains.set == nil {
			// Start-function and construction callbacks can run before a public
			// invocation lease exists. Their native entry is already serialized, so
			// there is no complete-call lease to suspend or restore.
			return gcInvocationSuspension{}
		}
		panic("wago: partial Runtime GC invocation lease ownership")
	}
	domains.release(owner)
	domains.unlock()
	if dynamic {
		topology.gate.RUnlock()
	}
	return gcInvocationSuspension{in: in, topology: topology, domains: domains, owner: owner, dynamic: dynamic, active: true}
}

func (s *gcInvocationSuspension) resume() {
	if s == nil || !s.active {
		return
	}
	s.active = false
	domains := s.domains
	if s.dynamic {
		s.topology.gate.RLock()
		domains = s.in.gcInvocationDomains()
	}
	domains.lock()
	domains.claim(s.owner)
}
