// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !tinygo && !wago_target_tinygo

package gc

import "sync/atomic"

// GC helper ownership is process-wide. Collectors retain granted helpers until
// teardown, but admission never waits: a collector with no grant still makes
// progress on its coordinator.
const maxProcessGCHelpers = 7

var processGCHelpers atomic.Int32

func claimProcessGCHelpers(wanted, limit int) int {
	if wanted <= 0 || limit <= 0 {
		return 0
	}
	limit = min(limit, maxProcessGCHelpers)
	for {
		owned := processGCHelpers.Load()
		available := int32(limit) - owned
		if available <= 0 {
			return 0
		}
		grant := min(int32(wanted), available)
		if processGCHelpers.CompareAndSwap(owned, owned+grant) {
			return int(grant)
		}
	}
}

func releaseProcessGCHelpers(count int) {
	if count == 0 {
		return
	}
	for {
		owned := processGCHelpers.Load()
		if count < 0 || int32(count) > owned {
			panic("gc: process helper budget underflow")
		}
		if processGCHelpers.CompareAndSwap(owned, owned-int32(count)) {
			return
		}
	}
}
