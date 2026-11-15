// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var ordinaryShiftedImmediateEnabled = os.Getenv("WAGO_ARM64_NO_ORDINARY_SHIFTED_IMMEDIATE") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE") != "0"

var shiftedALUComparePaddingEnabled = os.Getenv("WAGO_ARM64_NO_SHIFTED_ALU_COMPARE_PADDING") != "1"

var shiftedALUImmediateEnabled = os.Getenv("WAGO_ARM64_NO_SHIFTED_ALU_IMMEDIATE") != "1"

// Prefer scarce immutable leases for constants without an immediate form.
var shiftedImmediateHintFilterEnabled = os.Getenv("WAGO_ARM64_NO_SHIFTED_IMMEDIATE_HINT_FILTER") != "1"

func discountShiftedImmediateHint(consumer byte, value int64) bool {
	if !shiftedImmediateHintFilterEnabled || !shiftedAddSubImmediateEnabled || !ordinaryShiftedImmediateEnabled || !fitsShiftedAddSubImmediate(value) {
		return false
	}
	return consumer == 0x6a || consumer == 0x6b || consumer == 0x7c || consumer == 0x7d
}

// Reuse the existing scan's direct-load reuse bit as a cost hint, not as a
// semantic assertion about every value ever assigned to a mutable local.
func reusedAddressLocalMaskForHints(h *funcHintView) uint64 {
	if !shiftedALUImmediateEnabled || !h.flags.has(hintTouchesMemory) {
		return 0
	}
	var mask uint64
	for i, score := range h.localScore[:min(len(h.localScore), 64)] {
		if score&localScoreParamAddressReuse != 0 {
			mask |= uint64(1) << uint(i)
		}
	}
	return mask
}
