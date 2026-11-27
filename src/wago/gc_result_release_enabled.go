// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

type gcResultReleaseBatch struct {
	gcRoots []gcResultRoot
}

func (batch *gcResultReleaseBatch) claimGCResultRootsLocked(store *referenceStore) {
	if collectorRuntimeForImage {
		batch.gcRoots = store.takeObjectTokenRootsLocked()
	}
}

func (batch *gcResultReleaseBatch) releaseGCResultRoots(cleanup *resourceCleanup, clearGC func(gcResultRoot)) {
	for i, entry := range batch.gcRoots {
		batch.gcRoots[i] = gcResultRoot{}
		owner := entry.owner
		cleanup.run(func() { clearGC(entry) })
		cleanup.run(owner.releaseResourceRoot)
	}
}

func gcResultRootMetadataReleaser() func(gcResultRoot) {
	if !collectorRuntimeForImage {
		return nil
	}
	return releaseGCResultRootMetadata
}

func releaseGCResultRootMetadata(entry gcResultRoot) {
	releaseGCResultRootMetadataWith(entry, clearGCResultRootSlot)
}

func clearGCResultRootSlot(owner *Instance, slot uint32) error {
	if owner.gc != nil {
		return owner.gc.SetGlobalSlot(slot, gc.Null())
	}
	return nil
}

func releaseGCResultRootMetadataWith(entry gcResultRoot, clearSlot func(*Instance, uint32) error) {
	state := entry.owner.existingPublicGCState()
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	slots := state.resultSlots()
	if slots.matches(entry.ownerIndex, entry.token, entry.slot) {
		_ = clearSlot(entry.owner, entry.slot)
		slots.clear(entry.ownerIndex)
	}
}

// A converted collector value can own one public root through its extern slot.
// The root receipt is released after store.mu, like an ordinary GC token.
type rootedExternrefValue interface {
	externrefGCRoot() gcResultRoot
	externrefPublicValue() any
}

type compactExternResultRoot struct {
	root gcResultRoot
}

func claimCompactExternResultRoot(value any) compactExternResultRoot {
	if rooted, ok := value.(rootedExternrefValue); ok {
		return compactExternResultRoot{root: rooted.externrefGCRoot()}
	}
	return compactExternResultRoot{}
}

func (r compactExternResultRoot) appendTo(batch *gcResultReleaseBatch) bool {
	if r.root.owner == nil {
		return false
	}
	batch.gcRoots = append(batch.gcRoots, r.root)
	return true
}

func (r compactExternResultRoot) release() {
	if r.root.owner != nil {
		releaseRootedExternref(r.root)
	}
}

func releaseRootedExternref(root gcResultRoot) {
	unlockNative := lockNativeExecutionForHostAccess()
	domain := root.owner.lockGCCollector()
	var cleanup resourceCleanup
	cleanup.run(func() { releaseGCResultRootMetadata(root) })
	cleanup.run(func() { unlockGCCollector(domain) })
	cleanup.run(unlockNative)
	cleanup.run(root.owner.releaseResourceRoot)
	cleanup.finish()
}
