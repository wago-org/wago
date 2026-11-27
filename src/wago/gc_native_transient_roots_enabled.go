// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

func (r *gcNativeFrameRoots) syncGlobalsBeforeCollection() {
	if r == nil || r.frameLayout&gcNativeFrameSyncGlobalRoots == 0 || r.owner == nil || r.suspended == nil {
		return
	}
	if err := r.owner.syncGenericGCGlobalRootsLocked(r.suspended); err != nil {
		panic(gcHelperFailure(err))
	}
	// Native execution remains parked for the complete allocation attempt. One
	// synchronization therefore covers a minor collection, root rewrites, and a
	// possible full-collection retry. The next helper republishes this flag.
	r.frameLayout &^= gcNativeFrameSyncGlobalRoots
}

func (r *gcNativeFrameRoots) rangeSuspendedRoots(owner *Instance, state *gcPublicState, fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	if state != nil && state.hostRootPlan != nil {
		for i := int(state.hostActivationCount) - 1; i >= 0; i-- {
			activation := &state.hostActivations[i]
			if activation.noFrame {
				continue
			}
			if int(activation.callsite) >= len(state.hostRootPlan.callsites) {
				panic(gcHelperFailuref("generic GC host activation callsite %d is unavailable", activation.callsite))
			}
			callsite := &state.hostRootPlan.callsites[activation.callsite]
			chain := gcNativeFrameRoots{
				owner:                owner,
				base:                 activation.base,
				offsets:              callsite.offsets,
				frameBytes:           callsite.frameBytes,
				frameLayout:          r.frameLayout,
				allowExternalReturn:  r.allowExternalReturn,
				codeBase:             state.hostCodeBase,
				codeBytes:            state.hostCodeBytes,
				adapterReturnOffsets: state.hostRootPlan.adapterReturnOffsets,
				callsites:            state.hostRootPlan.callsites,
			}
			if !chain.rangeChain(fn, sink, activation.stack) {
				return false
			}
		}
	}
	return true
}

func (r *gcNativeFrameRoots) rangeTransientRoots(fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	r.syncGlobalsBeforeCollection()
	if !r.rangeChain(fn, sink, r.physicalStack) {
		return false
	}
	if !r.rangeSuspendedRoots(r.owner, r.suspended, fn, sink) {
		return false
	}
	if r.domainHostRoots && r.owner != nil && r.owner.refStore != nil && !r.owner.refStore.rangeGCDomainSuspendedRoots(r.owner.gc, r.suspended, r, fn, sink) {
		return false
	}

	return true
}

// The native entry lease and collector-domain invocation lease keep parked
// activations stable. Snapshot owners under the store lock, then release it
// before walking chains: a foreign return can look up another owner in this store.
func (s *referenceStore) rangeGCDomainSuspendedRoots(collector *gc.Collector, exclude *gcPublicState, roots *gcNativeFrameRoots, fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	var owners [gcHostActivationLimit]gcSuspendedRootOwner
	var extra []gcSuspendedRootOwner
	if roots.suspended != nil {
		extra = roots.suspended.suspendedOwnerScratch()
	}
	n := 0
	s.mu.Lock()
	for owner, registration := range s.instances {
		if registration == nil || registration.resourcesReleased || owner == nil || owner.gc != collector {
			continue
		}
		state := owner.existingPublicGCState()
		if state == nil || state == exclude || state.hostActivationCount == 0 {
			continue
		}
		if n < len(owners) {
			owners[n] = gcSuspendedRootOwner{owner, state}
		} else {
			extra = append(extra, gcSuspendedRootOwner{owner, state})
		}
		n++
	}
	s.mu.Unlock()
	if roots.suspended != nil {
		roots.suspended.retainSuspendedOwnerScratch(extra)
	}
	defer clear(extra)
	for i := 0; i < min(n, len(owners)); i++ {
		if !roots.rangeSuspendedRoots(owners[i].owner, owners[i].state, fn, sink) {
			return false
		}
	}
	for _, item := range extra {
		if !roots.rangeSuspendedRoots(item.owner, item.state, fn, sink) {
			return false
		}
	}
	return true
}
