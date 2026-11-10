// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import "fmt"

// EnableCheckedHandles enables generation bookkeeping for the checked Go API.
// Trusted JIT collectors omit it and retain the compact native ABI. Enable only
// before allocation; native allocation batches must not be exposed afterward.
func (c *Collector) EnableCheckedHandles() error {
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if len(c.handles) != 1 || c.stats.Allocations != 0 {
		return fmt.Errorf("gc: checked handles require a new collector")
	}
	generations := make([]uint64, 1)
	c.checkedHandles = &generations
	return nil
}

// CheckedIdentity is solely for the checked parent-package adapter. Ref is a
// raw, collector-local native value here; importing native opts into that trust.
func (c *Collector) CheckedIdentity(ref Ref) (uint64, error) {
	if _, err := c.refDesc(ref); err != nil {
		return 0, err
	}
	h := handleOf(ref)
	if c.checkedHandles == nil || int(h) >= len(*c.checkedHandles) {
		return 0, fmt.Errorf("gc: generation bookkeeping unavailable")
	}
	return (*c.checkedHandles)[h], nil
}
