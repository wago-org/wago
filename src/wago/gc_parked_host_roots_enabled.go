// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

// Public object tokens are issued for Wasm GC objects and RV64 owned exceptions.
const collectorObjectTokensBuilt = collectorRuntimeForImage

type parkedHostRootCache struct {
	entries *[gcHostActivationLimit]*parkedHostRoots
}

func (c *parkedHostRootCache) parkedRoots(index uint8) *parkedHostRoots {
	if c.entries == nil {
		return nil
	}
	return c.entries[index]
}

// ensureParkedRoots is called with the public state mutex held. Retain each
// journal across callbacks so its empty collector slots can be reused.
func (c *parkedHostRootCache) ensureParkedRoots(index uint8) *parkedHostRoots {
	if c.entries == nil {
		c.entries = new([gcHostActivationLimit]*parkedHostRoots)
	}
	roots := &c.entries[index]
	if *roots == nil {
		*roots = &parkedHostRoots{}
	}
	return *roots
}
