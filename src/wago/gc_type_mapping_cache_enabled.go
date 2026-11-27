// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

type compiledGCTypeMappingState struct {
	gcTypeMapping *gcTypeMappingCacheEntry
}

// A Compiled retains only its most recently used domain mapping. Instances own
// their immutable mapping independently, so replacement cannot invalidate a
// live instance and retired domains cannot accumulate in a long-lived Compiled.
type gcTypeMappingCacheEntry struct {
	domainID        uint64
	domainTypeCount int
	mapping         *gcTypeMapping
}

func (c *Compiled) cachedGCTypeMapping(domainID uint64, domainTypeCount int) *gcTypeMapping {
	if c == nil || domainID == 0 {
		return nil
	}
	cache := c.loadCodeCache()
	indexes := c.loadCompileIndexes()
	if cache == nil || indexes == nil {
		return nil
	}
	cache.mu.Lock()
	entry := indexes.gcTypeMapping
	cache.mu.Unlock()
	if entry == nil || entry.domainID != domainID || entry.domainTypeCount != domainTypeCount {
		return nil
	}
	return entry.mapping
}

func (c *Compiled) rememberGCTypeMapping(domainID uint64, domainTypeCount int, mapping *gcTypeMapping) {
	if c == nil || domainID == 0 || mapping == nil {
		return
	}
	c.ensureCodeCache()
	cache := c.loadCodeCache()
	indexes := c.ensureCompileIndexes()
	if indexes == nil {
		return
	}
	cache.mu.Lock()
	previous := indexes.gcTypeMapping
	if previous != nil && previous.domainID == domainID && previous.domainTypeCount == domainTypeCount && previous.mapping == mapping {
		cache.mu.Unlock()
		return
	}
	if previous == nil || previous.domainID != domainID || previous.domainTypeCount <= domainTypeCount {
		indexes.gcTypeMapping = &gcTypeMappingCacheEntry{domainID: domainID, domainTypeCount: domainTypeCount, mapping: mapping}
	}
	cache.mu.Unlock()
}
