// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// A forward-only suffix without another read makes the local unobservable.
// Reject branches and new loops: they could revisit a read before this suffix.
// An inline reader ends at its callee boundary; the next splice reinitializes
// its private locals before any new read.
func (f *fn) remainderLocalUnread(reg Reg, after ...*wasm.Reader) bool {
	if len(after) != 1 || after[0] == nil || f.inlineForwardN != 0 {
		return false
	}
	end := len(f.locals)
	if f.localBase == 0 {
		end = f.nLocals
	}
	local := -1
	for x := f.localBase; x < end; x++ {
		r, floating, pinned := f.pinReg(x)
		if pinned && !floating && r == reg {
			if local != -1 {
				return false
			}
			local = x - f.localBase
		}
	}
	if local < 0 {
		return false
	}
	suffix := *after[0]
	start := suffix.Offset()
	var imm wasm.InstructionImmediate
	for n := 0; n < 128 && suffix.Offset()-start < 512; n++ {
		op, err := suffix.Byte()
		if err != nil {
			return false
		}
		if !(op == 0x01 || op == 0x02 || op == 0x04 || op == 0x05 || op == 0x0b || op >= 0x1a && op <= 0x1c || op >= 0x20 && op <= 0x22 || op >= 0x41 && op <= 0xc4) || f.classifier.ClassifyInto(&suffix, op, &imm) != nil {
			return false
		}
		if op == 0x20 && imm.Index == uint32(local) {
			return false
		}
		if !suffix.HasNext() {
			return op == 0x0b
		}
	}
	return false
}
