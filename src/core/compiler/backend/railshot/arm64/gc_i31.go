// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func (f *fn) tryFuseI31RoundTrip(r *wasm.Reader) bool {
	next := *r
	op, err := next.Byte()
	if err != nil || op != 0xfb {
		return false
	}
	get, err := next.U32()
	if err != nil || (get != 29 && get != 30) {
		return false
	}
	// The tagged intermediate never escapes, so it cannot be null. Keep
	// earlier input traps in order before extracting the low 31 integer bits.
	f.materializePendingTraps()
	value := f.materialize(f.popValue())
	if get == 29 {
		f.a.LslImm(value, value, 1, true)
		f.a.AsrImm(value, value, 1, true)
	} else {
		f.a.AndImm32(value, value, 0x7fffffff)
	}
	f.pushReg(value, mtI32)
	*r = next
	f.stats.peep("i31-roundtrip")
	return true
}
