// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

func railshotCanonicalForeignFuncref(store *referenceStore, source *Instance, descriptor uint64) (*Instance, uint64, bool) {
	var owner *Instance
	if referenceSignaturesBuilt {
		owner = rv64GlobalDescriptorOwner(source, descriptor)
	}
	if owner == nil {
		owner = rv64ImportedResultDescriptorOwner(store, source, descriptor)
	}
	if owner == nil && store != nil {
		// A mutable cell can contain a token-issued function from a producer
		// outside its container's frozen import graph. The store identity owns
		// that producer's physical root. The caller holds store.mu.
		if entry := store.byIdentity[funcrefIdentity{descriptor: descriptor}]; entry != nil && entry.descriptor == descriptor {
			owner = entry.owner
		}
	}
	if owner == nil || owner == source || owner.refStore != store || !store.hasInstanceResourcesLocked(owner) {
		return nil, 0, false
	}
	if _, valid := rv64PublishedDescriptor(owner, descriptor); !valid {
		return nil, 0, false
	}
	return store.canonicalFuncrefOwnerLocked(owner, descriptor)
}

// Frozen function attachments retain each reachable producer while the source
// is retained. Establish membership in those owned descriptor arenas before
// reading publication fields. This path also covers a producer's first result
// after logical close, without searching unrelated registered instances.
// The caller holds store.mu and the source's invocation or attachment lease.
func rv64ImportedResultDescriptorOwner(store *referenceStore, source *Instance, descriptor uint64) *Instance {
	if store == nil || source == nil || source.refStore != store || descriptor == 0 {
		return nil
	}
	inline := [8]*Instance{source}
	owners := inline[:1]
	var ownerIndex map[*Instance]struct{}
	edges := 0
	for next := 0; next < len(owners); next++ {
		parent := owners[next]
		for index := 0; index < parent.c.NumImports; index++ {
			edges++
			if edges > rv64MaxTableLeaseEdges {
				return nil
			}
			export := parent.importReferenceNative(index)
			if export == nil {
				// Table-free relays have no descriptor directory. Their sealed
				// resolved binding still owns the same import attachment.
				export, _ = rv64NativeFunctionAttachment(parent, index)
			}
			if export == nil || export.inst == nil {
				continue
			}
			owner := export.inst
			if owner.refStore != store || !store.hasInstanceResourcesLocked(owner) {
				continue
			}
			if _, own := owner.funcrefDescriptorIndex(descriptor); own {
				if _, valid := rv64PublishedDescriptor(owner, descriptor); valid {
					return owner
				}
				return nil
			}
			var ok bool
			owners, ok = rv64AppendOwnerForWalk(owners, &ownerIndex, owner)
			if !ok {
				return nil
			}

		}
	}
	return nil
}
