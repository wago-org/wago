// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// arrayLenShortcut consumes the already-canonical helper argument. Validation
// proved arrayref; the shared resolver checks representation, liveness and both
// handle/header extents. Failed checks retain the helper's precise traps.
func (f *fn) arrayLenShortcut() int {
	f.a.Load64(RAX, R8, hcArgs)
	f.a.MovImm32(RCX, int32(gc.HeaderSize))
	if regallocCheckEnabled {
		f.checkCallClobber()
	}
	site := f.a.CallRel32()
	f.relocs = append(f.relocs, f.newGCStubCallReloc(site, gcSharedStubResolveArrayObject))
	f.stats.call("gcnative-leaf")
	f.a.TestSelf(RDX, false)
	f.stats.peep("gc-array-len-native")
	return f.a.JccPlaceholder(condE)
}
