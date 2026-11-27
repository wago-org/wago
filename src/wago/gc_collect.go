// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"errors"
	"fmt"
	"sync"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var errGenericGCRootState = errors.New("generic GC boundary collection has no registered root state")

func drainGCMetadataReclaims(collector *gc.Collector) {
	drainCompactExternReclaims(collector)
	drainExceptionReclaims(collector)
}

// CollectGC performs one full collection for this instance's exact Runtime GC
// domain. It is safe to call from a synchronous host import: parked native
// activations, globals, tables, retained public references, and same-domain
// instances remain rooted for the duration of the collection.
//
// Instances without a live WasmGC collector return an error. Collection remains
// serialized with native execution and collector-domain mutation, and fails with
// ErrPermissionDenied while callback-scoped guest storage is borrowed.
func (in *Instance) CollectGC() error {
	if err := in.beginInvocation(); err != nil {
		return err
	}
	defer in.endInvocation()
	if managedFunctionLifetimeBuilt {
		defer func() { drainManagedFunctionStoreReclaims(in.refStore) }()
	}
	if !nativeCollectorFrameRootsBuilt || in == nil || in.gc == nil || in.c == nil {
		return fmt.Errorf("wago: instance has no live WasmGC collector")
	}
	if in.guestStorageBorrowed() {
		return fmt.Errorf("wago: collection is unavailable while guest storage is borrowed: %w", ErrPermissionDenied)
	}
	in.revokePrivateGCEntry()
	// Public collection is an independent operation, not a callback-scoped
	// re-entry. Use a fresh identity instead of racing the instance's active
	// invocation ID; GCHostModule.CollectGC handles lease ownership explicitly.
	invocation := in.lockManagedFunctionCollectionInvocation(newInvocationID())
	defer invocation.unlock()
	return in.collectGCWithJoint(invocation.isDynamic())
}

func (in *Instance) collectGC() error {
	return in.collectGCWithJoint(in.executionFlags.Load()&executionFlagDynamicGCDomain != 0)
}

func (in *Instance) collectGCWithJoint(topologyLeased bool) error {
	if !nativeCollectorBuilt {
		return fmt.Errorf("wago: this build has no Wasm collector")
	}
	if !nativeCollectorFrameRootsBuilt {
		return errGenericGCRootState
	}
	public := in.existingPublicGCState()
	if public == nil {
		return errGenericGCRootState
	}
	var nativeMu *sync.Mutex
	if in.usesIndependentExecution() {
		nativeMu = in.independentNativeExecutionMu()
	} else if in.c.threadedMemory0() {
		nativeMu = &in.memoryDir.nativeMu
	} else {
		nativeMu = &nativeExecutionMu
	}
	nativeMu.Lock()
	defer nativeMu.Unlock()
	defer drainGCMetadataReclaims(in.gc)
	if managedFunctionLifetimeBuilt {
		if handled, err := collectManagedFunctionGraph(in, public, topologyLeased); handled {
			return err
		}
	}
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	public.mu.Lock()
	defer public.mu.Unlock()
	if err := in.prepareGCCollectionRootsLocked(public); err != nil {
		return err
	}
	return in.gc.CollectFull(&public.frameRoots)
}

// prepareGCCollectionRootsLocked prepares the standard root universe while
// native execution, the collector domain, and public.mu are held.
func (in *Instance) prepareGCCollectionRootsLocked(public *gcPublicState) error {
	if err := in.syncGenericGCGlobalRootsLocked(public); err != nil {
		return err
	}
	public.frameRoots = in.gcCollectFrameRoots(public)
	return nil
}

// collectGenericGCAtBoundary runs the fallback collection between native entries.
func (in *Instance) collectGenericGCAtBoundary() error {
	if !wasmGCBuilt || in == nil || in.gc == nil || in.c == nil || !in.c.genericGCBoundaryCollectionSafe() {
		return nil
	}
	// Exact native roots permit allocation-pressure and stress collections.
	// Keep the boundary fallback when native collection is unavailable.
	if in.c.genericGCFrameRoots() != nil && in.gc.CollectsOnAllocation() {
		return nil
	}
	return in.collectGC()
}
