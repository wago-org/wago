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
