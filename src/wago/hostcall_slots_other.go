// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !arm64 || tinygo

package wago

import "fmt"

func (c HostCall) paramSlotIndex(i int, want ValType) int {
	if i == 0 && c.sig != nil && len(c.sig.Params) != 0 {
		got := c.sig.Params[0]
		if got != want {
			panic(fmt.Sprintf("wago: host parameter %d is %s, not %s", i, got, want))
		}
		return 0
	}
	got := c.paramType(i)
	if got != want {
		panic(fmt.Sprintf("wago: host parameter %d is %s, not %s", i, got, want))
	}
	// Equal logical and physical counts mean every value occupies one slot.
	// Read the current view instead of caching offsets in public signatures.
	if c.params.len() == len(c.sig.Params) {
		return i
	}
	return hostCallSlot(c.sig.Params, i)
}

func (c HostCall) resultSlotIndex(i int, want ValType) int {
	if i == 0 && c.sig != nil && len(c.sig.Results) != 0 {
		got := c.sig.Results[0]
		if got != want {
			panic(fmt.Sprintf("wago: host result %d is %s, not %s", i, got, want))
		}
		return 0
	}
	got := c.resultType(i)
	if got != want {
		panic(fmt.Sprintf("wago: host result %d is %s, not %s", i, got, want))
	}
	if c.results.len() == len(c.sig.Results) {
		return i
	}
	return hostCallSlot(c.sig.Results, i)
}
