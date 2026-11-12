// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// A successful collector cast preserves the reference and proves the same
// adjacent cast cannot fail. Keep the first check and expose any following
// access to the ordinary cast/load fusion. No source call or root mask is lost.
func (f *fn) skipIdenticalGCCasts(r *wasm.Reader, sub uint32, heap int64, exact bool) {
	for {
		if op, ok := r.Peek(); !ok || op != 0xfb {
			return
		}
		next := *r
		_, _ = next.Byte()
		other, err := next.U32()
		if err != nil || other != sub {
			return
		}
		target, targetExact, err := readRefHeapTypeImmediate(&next)
		if err != nil || target != heap || targetExact != exact {
			return
		}
		*r = next
		f.stats.peep("gc-identical-cast-elide")
	}
}
