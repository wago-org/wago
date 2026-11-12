// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// A successful ordinary allocation returns a non-null reference whose dynamic
// type is the allocated absolute type. An immediately following ordinary cast
// to that same type therefore preserves the value and cannot trap. Decode on a
// copy so every rejected or malformed candidate leaves the source untouched.
func consumeSameTypeGCAllocationCast(r *wasm.Reader, typeIndex uint32) bool {
	next := *r
	op, err := next.Byte()
	if err != nil || op != 0xfb {
		return false
	}
	sub, err := next.U32()
	if err != nil || sub != 22 { // non-nullable ref.cast only
		return false
	}
	target, exact, err := readRefHeapTypeImmediate(&next)
	if err != nil || exact || target < 0 || uint64(target) != uint64(typeIndex) {
		return false
	}
	*r = next
	return true
}

func (f *fn) skipSameTypeGCAllocationCast(r *wasm.Reader, typeIndex uint32) {
	if !consumeSameTypeGCAllocationCast(r, typeIndex) {
		return
	}
	// Match the state transitions that emitFB would perform for subopcode 22.
	f.prepareGCResolvedFB(22)
	f.prepareGCFreshStructFB(22)
	f.stats.peep("gc-allocation-cast-elide")
}

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
