// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_gc || !wago_no_exception_handling)

package wago

const managedFunctionLifetimeBuilt = managedFunctionGraphForImage

// lifeMu is held by the physical root ledger. This cold lookup does not take
// another lock and cannot reverse the graph-to-lifetime commit lock order.
func managedFunctionRootEpochLocked(in *Instance) {
	if !managedFunctionLifetimeBuilt || managedFunctionMemberCount.Load() == 0 {
		return
	}
	if value, ok := managedFunctionMembers.Load(in); ok {
		member := value.(*managedFunctionMember)
		member.graph.epoch.Add(1)
		if in.invocationState.Load()&instanceInvocationClosed != 0 {
			member.graph.pendingCollection.Store(true)
		}
	}
}

func managedFunctionRetainAllowedLocked(in *Instance) bool {
	if managedFunctionLifetimeBuilt && managedFunctionMemberCount.Load() != 0 {
		if value, ok := managedFunctionMembers.Load(in); ok {
			return !value.(*managedFunctionMember).collecting.Load()
		}
	}
	return true
}

// Registration must publish the graph before registerInstance takes store.mu.
// Mutable function containers need lifecycle deferral even when no heap closure
// has allocated a collector environment yet.
func managedFunctionPrepareRegistration(store *referenceStore, in *Instance) {
	if !managedFunctionLifetimeBuilt || store == nil || in == nil || !compiledManagedFunctionReachability(in.c) {
		return
	}
	if _, ok := managedFunctionGraphs.Load(store); ok {
		return
	}
	managedFunctionRegistryMu.Lock()
	defer managedFunctionRegistryMu.Unlock()
	managedFunctionGraphFor(store)
}

// These hooks run with store.mu held, never from a collector callback.
func managedFunctionRegisteredLocked(store *referenceStore, in *Instance) {
	if !managedFunctionLifetimeBuilt {
		return
	}
	if value, ok := managedFunctionGraphs.Load(store); ok {
		g := value.(*managedFunctionGraph)
		g.mu.Lock()
		if !g.retired {
			g.registerMemberLocked(in, true)
		}
		g.mu.Unlock()
	}
}
func managedFunctionLifetimeLocked(store *referenceStore, in *Instance, released bool) {
	if !managedFunctionLifetimeBuilt || managedFunctionMemberCount.Load() == 0 {
		return
	}
	if value, ok := managedFunctionGraphs.Load(store); ok {
		g := value.(*managedFunctionGraph)
		g.mu.Lock()
		if member := g.members[in]; member != nil && !g.retired {
			if released {
				member.registered = false
				if member.envPins == 0 {
					g.removeMemberLocked(member)
				}
			}
			g.epoch.Add(1)
		}
		g.mu.Unlock()
	}
}
