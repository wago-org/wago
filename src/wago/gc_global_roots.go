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

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var (
	errGenericGCRootMapping = errors.New("generic GC boundary collection has an invalid global root mapping")
	errGenericGCNonCompact  = errors.New("generic GC boundary collection found a non-compact reference")
)

type gcGlobalRootMapping struct {
	GlobalIndex uint32
	SlotIndex   uint32
}

// installGlobalRootMappings completes the private construction-time directory.
func (in *Instance) installGlobalRootMappings(public *gcPublicState, mappings []gcGlobalRootMapping) error {
	for i, g := range in.c.Globals {
		if !isGCRefValType(g.Type) || i >= len(in.globalCells) || in.globalCells[i] == nil {
			continue
		}
		mapped := false
		for _, mapping := range mappings {
			if mapping.GlobalIndex == uint32(i) {
				mapped = true
				break
			}
		}
		if mapped {
			continue
		}
		bits := readGlobalObject(in.globalCells[i], g.Type)
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			return fmt.Errorf("global %d contains non-compact generic GC reference %#x", i, bits)
		}
		slot, err := in.gc.NewCheckedGlobalSlot(ref)
		if err != nil {
			return fmt.Errorf("global %d generic GC root: %w", i, err)
		}
		mappings = append(mappings, gcGlobalRootMapping{GlobalIndex: uint32(i), SlotIndex: slot})
	}
	public.setGlobalRootMappings(mappings)
	return nil
}

// syncGenericGCGlobalRootsLocked mirrors native object references into
// collector slots before a collection. The caller holds the required locks.
func (in *Instance) syncGenericGCGlobalRootsLocked(public *gcPublicState) error {
	if !collectorObjectTokensBuilt || in == nil || in.gc == nil || public == nil {
		return nil
	}
	for _, mapping := range public.globalRootMappings() {
		if int(mapping.GlobalIndex) >= len(in.globalCells) || in.globalCells[mapping.GlobalIndex] == nil {
			return errGenericGCRootMapping
		}
		typ := ValAnyRef
		if int(mapping.GlobalIndex) < len(in.c.Globals) {
			typ = in.c.Globals[mapping.GlobalIndex].Type
		}
		bits := readGlobalObject(in.globalCells[mapping.GlobalIndex], typ)
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			return errGenericGCNonCompact
		}
		if err := in.gc.SetGlobalSlot(mapping.SlotIndex, ref); err != nil {
			return err
		}
	}
	return nil
}

// reconcileGCGlobalRoots synchronizes exact staged mutable GC global cells with
// their checked collector slots after every successful native invocation. Native
// allocation can now satisfy several constructors without a Go helper boundary,
// so helper-triggered synchronization alone is no longer authoritative.
func (in *Instance) reconcileGCGlobalRoots() error {
	if !collectorObjectTokensBuilt || in == nil || in.gc == nil || in.c == nil || !in.c.hasGCRefGlobals() {
		return nil
	}
	var staged []gcGlobalRootMapping
	if wasmGCBuilt {
		if state := in.pluginState.Load(); state != nil {
			staged = state.stagedGlobalRoots()
		}
	}
	public := in.existingPublicGCState()
	hasGeneric := public != nil && len(public.globalRootMappings()) != 0
	if !hasGeneric && len(staged) == 0 {
		return nil
	}
	if public == nil {
		public = in.publicGCState()
	}
	public.mu.Lock()
	defer public.mu.Unlock()
	if hasGeneric {
		if err := in.syncGenericGCGlobalRootsLocked(public); err != nil {
			return err
		}
	}
	for i, mapping := range staged {
		if int(mapping.GlobalIndex) >= len(in.globalCells) || in.globalCells[mapping.GlobalIndex] == nil {
			return fmt.Errorf("GC global root mapping %d names unavailable global %d", i, mapping.GlobalIndex)
		}
		bits := readGlobalObject(in.globalCells[mapping.GlobalIndex], ValAnyRef)
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			return fmt.Errorf("GC global %d contains non-compact reference %#x", mapping.GlobalIndex, bits)
		}
		if err := in.gc.SetGlobalSlot(mapping.SlotIndex, ref); err != nil {
			return fmt.Errorf("GC global %d root slot %d: %w", mapping.GlobalIndex, mapping.SlotIndex, err)
		}
	}
	return nil
}

// reconcileGCGlobalRootsAfter publishes guest global writes even when native
// execution traps or is interrupted. A trap unwinds the native frame, so the
// global cell is the only remaining owner of a reference stored before failure.
// Keep the execution failure first when root maintenance also fails.
func (in *Instance) reconcileGCGlobalRootsAfter(primary error) error {
	if !collectorObjectTokensBuilt {
		return primary
	}
	return joinPrimary(primary, in.reconcileGCGlobalRoots())
}

//lint:ignore U1000 retained for feature-gated GC global admission checks
func (c *Compiled) hasGCRefGlobals() bool {
	if !collectorObjectTokensBuilt || c == nil {
		return false
	}
	for _, global := range c.Globals {
		if isGCRefValType(global.Type) {
			return true
		}
	}
	return false
}
