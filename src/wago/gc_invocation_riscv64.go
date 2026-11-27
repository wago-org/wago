// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_reference_store && !wago_standalone_no_collector && linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_gc || !wago_no_exception_handling)

package wago

// Explicit collection through a numeric module can span a Runtime's managed
// edges. This private captured lease has no requesting-instance refresh: its
// topology read lock keeps the exact ordered domain list fixed until unlock.
func (in *Instance) lockManagedFunctionCollectionInvocation(id invocationID) gcInvocationLease {
	if in == nil || in.refStore == nil {
		return gcInvocationLease{}
	}
	if !managedFunctionLifetimeBuilt {
		return in.lockGCInvocation(id)
	}
	if _, ok := managedFunctionGraphs.Load(in.refStore); !ok {
		return in.lockGCInvocation(id)
	}
	topology := in.refStore.ensureGCTopology()
	topology.gate.RLock()
	in.refStore.mu.Lock()
	var set gcInvocationDomainSet
	for _, entry := range in.refStore.instances {
		if entry != nil && !entry.resourcesReleased {
			set.add(entry.gcRegistration().domain)
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
		topology.gate.RUnlock()
		if !allBorrowed {
			return in.lockGCInvocation(id)
		}
		return gcInvocationLease{domains: domains, owner: id, dynamic: true}
	}
	domains.lock()
	domains.claim(id)
	return gcInvocationLease{topology: topology, domains: domains, owner: id, acquired: true, dynamic: true}
}

// Close can run from a parked host callback. It must never wait for that
// callback's own domain gate. Acquire an ordered, frozen set without waiting;
// an active invocation leaves the ordinary live-importer error unchanged.
func compactExternTryCloseInvocation(store *referenceStore) (gcInvocationLease, bool) {
	if managedFunctionInvocationActive(store) {
		return gcInvocationLease{}, false
	}
	topology := store.ensureGCTopology()
	if !topology.gate.TryRLock() {
		return gcInvocationLease{}, false
	}
	store.mu.Lock()
	var set gcInvocationDomainSet
	for _, entry := range store.instances {
		if entry != nil && !entry.resourcesReleased {
			set.add(entry.gcRegistration().domain)
		}
	}
	compactExternAddClaimedDomainsLocked(store, &set)
	store.mu.Unlock()
	set.sort()
	domains := gcInvocationDomainView{set: &set}
	for i := 0; i < domains.len(); i++ {
		gate := &domains.at(i).invocationMu
		if !gate.state.CompareAndSwap(0, invocationGateHeld) && !gate.state.CompareAndSwap(invocationGateRevoked, invocationGateRevoked|invocationGateHeld) {
			for j := i - 1; j >= 0; j-- {
				domains.at(j).invocationMu.Unlock()
			}
			topology.gate.RUnlock()
			return gcInvocationLease{}, false
		}
	}
	id := newInvocationID()
	domains.claim(id)
	return gcInvocationLease{topology: topology, domains: domains, owner: id, acquired: true, dynamic: true}, true
}
