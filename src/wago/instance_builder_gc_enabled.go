// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import (
	"errors"
	"fmt"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type instanceBuilderGCState struct {
	state instanceCollectorState
}

func (s *instanceBuilderGCState) collectorBuildState() *instanceCollectorState {
	return &s.state
}

func (b *instanceBuilder) prepareCollector() error {
	state := b.collectorBuildState()
	if compiledArchitecture != 4 && b.c.requiredFeatures&CoreFeatureGC == 0 {
		// validateCompiled already checked that collector-reference call
		// boundaries carry the native GC ABI. Check the remaining owners
		// directly for the common function-only module.
		cache := b.c.loadCodeCache()
		if cache != nil && !cache.hasGCObjectProducts() &&
			cache.gcMetadataFlags&nativeGCMetadataABIMask() == 0 && !gc.HasHeapObjectTypes(b.c.GCTypeDescs) {
			return nil
		}
	}
	needsExternConversion := wasmGCBuilt && b.c.stagedGCStructProduct().requiresExternConversion()
	needsHelpers := b.c.usesGCStructHelpers() || b.c.usesGCArrayHelpers()
	needsRuntimeDomain := b.c.needsRuntimeGCCollectorDomain()
	if b.c.hasOpaqueExternFrames() && !needsRuntimeDomain {
		preferred, err := preferredGCCollectorFromImports(b.c, b.imports, b.opts.store)
		if err != nil {
			return err
		}
		needsRuntimeDomain = preferred != nil
	}
	if !needsRuntimeDomain && !needsHelpers && ((!gc.HasHeapObjectTypes(b.c.GCTypeDescs) && !needsExternConversion) || (!needsExternConversion && (b.c.collectorFreeStructuralMetadata() || b.c.stagedGCTypeSubtypingProduct() != 0 || b.c.stagedGCI31Product() != 0 || b.c.collectorFreeGCArrayMetadata()))) {
		return nil
	}
	gcConfig := b.opts.GC
	if wasmGCBuilt && b.c.usesGenericGCExecution() && b.c.genericGCFrameRoots() == nil && !(rv64LegacyGCProductsForImage && rv64FixedRootlessGCProduct(b.c)) {
		// Generic products outside the validated native root-map slice still do
		// not publish every native reference at allocating helper safepoints. Keep
		// them stable and bounded rather than scanning an incomplete frame. The
		// exact RV64 allocating products have no live native reference across an
		// allocating helper safepoint; ref.eq publishes table roots separately,
		// while the local struct/array products allocate before producing a ref.
		gcConfig.Profile = gc.ProfileThroughput
		gcConfig.DisableCollection = true
	}
	// Atomic waits park while holding an instance-local native execution lease.
	// Until their compiler callsites publish exact GC frame roots, keep GC+Threads
	// modules in private collector domains so a same-memory notifier never waits
	// behind the parked invocation's Runtime-wide collector lease.
	if b.opts.store != nil && !b.opts.store.private && needsRuntimeDomain && b.c.sharedGCPersistentDomainSafe() && !b.c.usesAtomicWaitHelpers() {
		preferred, err := preferredGCCollectorFromImports(b.c, b.imports, b.opts.store)
		if err != nil {
			return err
		}
		collector, mapping, err := b.opts.store.acquireGCCollector(b.opts.startContext, gcConfig, b.c, preferred)
		if err != nil {
			return err
		}
		state.collector = collector
		state.shared = true
		state.domain = b.opts.store.gcDomainForCollector(collector)
		if state.domain != nil {
			state.domainID = state.domain.id
		}
		if state.domainID == 0 {
			b.opts.store.releaseUnclaimedGCCollector(collector)
			state.collector = nil
			state.shared = false
			return errors.New("wago: Runtime GC domain has no native identity")
		}
		state.typeMap = mapping
		return nil
	}
	descs := b.c.GCTypeDescs
	var mapping *gcTypeMapping
	if wasmGCBuilt && len(b.c.Types) == len(descs) && hasEquivalentLocalGCHeapTypes(b.c) {
		var err error
		mapping, descs, _, err = gcCanonicalTypePlan(b.c, nil, nil, true)
		if err != nil {
			return err
		}
	}
	if b.opts.store != nil && (!b.opts.store.private || compiledManagedFunctionReachability(b.c)) {
		if err := b.opts.store.bindFuncrefGCStore(); err != nil {
			return err
		}
	}
	collector, err := gc.NewCollector(gcConfig, descs)
	if err != nil {
		return err
	}
	collector.UseOwnedThroughputBacking()
	state.collector = collector
	state.typeMap = mapping
	state.domainID = newGCDomainIdentity()
	return nil
}

func (b *instanceBuilder) prepareNativeGCView() (*gc.NativeInstanceView, error) {
	state := b.collectorBuildState()
	localTypes := make([]gc.TypeID, len(b.c.Types))
	for local := range localTypes {
		domain, ok := state.typeMap.domain(uint32(local))
		if !ok {
			return nil, fmt.Errorf("instantiate: native GC type %d has no canonical domain mapping", local)
		}
		localTypes[local] = domain
	}
	view, err := b.buildNativeGCInstanceView(localTypes)
	if err != nil {
		return nil, wrapContextError("instantiate: native GC metadata view", err)
	}
	return view, nil
}

func (b *instanceBuilder) buildNativeGCInstanceView(localTypes []gc.TypeID) (*gc.NativeInstanceView, error) {
	state := b.collectorBuildState()
	if state.domain != nil && !state.constructionLocked {
		state.domain.invocationMu.Lock()
		defer state.domain.invocationMu.Unlock()
	}
	return gc.BuildNativeInstanceView(state.collector, localTypes)
}

func (b *instanceBuilder) rollbackCollector(cleanup *resourceCleanup) {
	state := b.collectorBuildState()
	if state.collector != nil {
		cleanup.run(func() {
			if state.shared && b.opts.store != nil && (b.registeredInstance == nil || b.registeredInstance.refStore == nil) {
				b.opts.store.releaseUnclaimedGCCollector(state.collector)
			} else if !state.shared {
				state.collector.Close()
			}
		})
	}
}
