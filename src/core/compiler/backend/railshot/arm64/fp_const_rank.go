// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var floatConstSlotRankingEnabled = os.Getenv("WAGO_ARM64_NO_FP_CONST_SLOT_RANKING") != "1"

type floatConstCandidate struct {
	typ  machineType
	bits int64
	n    int
}

// An unchanged hot entry must not hide decisive evidence for replacing the
// other entry. Keep its original register slot and the existing two-to-one
// threshold for the one changed lease. Disjoint pair changes use the old rule.
func rankSingleFloatConstReplacement(candidates []floatConstCandidate, first, best [2]int) [2]int {
	if first[1] < 0 || best[1] < 0 {
		return first
	}
	shared := 0
	stale := -1
	for slot, index := range first {
		if index == best[0] || index == best[1] {
			shared++
		} else {
			stale = slot
		}
	}
	if shared != 1 {
		return first
	}
	replacement := best[0]
	if replacement == first[0] || replacement == first[1] {
		replacement = best[1]
	}
	if candidates[replacement].n >= 2*candidates[first[stale]].n {
		first[stale] = replacement
	}
	return first
}
