// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import "slices"

const (
	handlesBackingDefault uint8 = iota
	handlesBackingFailed
	handlesBackingMapped
)

// Only capacity changes here. Callers publish new entries and the native view
// in the same order as the ordinary Go-backed allocator.
func (c *Collector) prepareHandleCapacity(extra int) bool {
	if cap(c.handles)-len(c.handles) >= extra {
		return true
	}
	if c.growOwnedHandles(len(c.handles) + extra) {
		return true
	}
	if c.handlesBacking != handlesBackingMapped {
		return false
	}
	next := slices.Grow(c.handles, extra)
	// The reservation covers every representable compact handle. Preserve the
	// ordinary allocator for any larger request, releasing the old map only after
	// its contents have been copied and before replacing the sole base pointer.
	c.releaseOwnedHandles()
	c.handlesBacking = handlesBackingFailed
	c.handles = next
	return true
}
