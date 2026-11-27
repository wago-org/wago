// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_reference_types || !wago_no_gc || !wago_no_exception_handling)

package wago

import (
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (c *Compiled) functionEnvironmentFrameRoots() bool {
	return c != nil && c.genericGCFrameRoots() != nil && c.usesManagedFunctionStorage()
}

// usesManagedFunctionStorage classifies validated runtime ownership needs. It
// grants no source, frame-root, or artifact authority by itself. Plain unused
// GC type declarations have no managed execution product or native GC ABI.
func (c *Compiled) usesManagedFunctionStorage() bool {
	return managedFunctionGraphForImage && c.hasManagedFunctionStorage()
}

func (c *Compiled) hasManagedFunctionStorage() bool {
	if !nativeCollectorFrameRootsBuilt || c == nil || c.nativeGCABIRequirement() != gc.NativeABIVersion {
		return false
	}
	if c.needsOwnedExceptionReferences() && c.requiredFeatures.IsEnabled(CoreFeatureExceptionHandling) {
		return true
	}
	if c.hasFunctionEnvironmentFrames() && (c.requiredFeatures.IsEnabled(CoreFeatureGC) || c.hasOpaqueExternFrames()) && (c.usesGenericGCExecution() || c.hasCollectorReferenceCallBoundary() || c.hasInternalCollectorReferenceFunctions() || c.hasOpaqueExternFrames()) {
		return true
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureGC) || !c.usesGenericGCExecution() {
		return false
	}
	for _, desc := range c.GCTypeDescs {
		if desc.Kind == gc.KindArray && (desc.Elem == gc.StorageFuncRef || desc.Elem == gc.StorageFuncRefNull) {
			return true
		}
		if desc.Kind == gc.KindStruct {
			for _, field := range desc.Fields {
				if field.Kind == gc.StorageFuncRef || field.Kind == gc.StorageFuncRefNull {
					return true
				}
			}
		}
	}
	return false
}

func compiledManagedFunctionReachability(c *Compiled) bool {
	return c.usesManagedFunctionStorage()
}

// collectManagedFunctionGraph runs only at an explicit full-collection boundary.
// A helper's incremental scheduling uses the same cursor separately; this full
// operation intentionally completes the fixed point before reclaiming a domain.
func collectManagedFunctionGraph(in *Instance, public *gcPublicState, topologyLeased bool) (bool, error) {
	if !nativeCollectorFrameRootsBuilt || managedFunctionMemberCount.Load() == 0 || !topologyLeased {
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
		domains.add(entry.gcRegistration().domain)
	}
	current := in.refStore.instances[in]
	var id invocationID
	if current != nil {
		if domain := current.gcRegistration().domain; domain != nil {
			domain.invocationState.Lock()
			id = domain.invocationOwner
			domain.invocationState.Unlock()
		}
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
	var groups map[*gc.Collector]gc.RootGroups
	if nativeCollectorFrameRootsBuilt {
		groups = make(map[*gc.Collector]gc.RootGroups, len(mark.domains))
		for _, owner := range owners {
			if owner.gc == nil {
				continue
			}
			p := owner.existingPublicGCState()
			if p == nil {
				continue
			}
			roots := owner.gcCollectFrameRoots(p)
			roots.omitDomainHostRoots() // each physical owner is represented exactly once
			groups[owner.gc] = append(groups[owner.gc], gc.RootGroup{Class: gc.RootNativeFrame, Roots: managedFunctionTransientRoots{roots: &roots}})
		}
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
