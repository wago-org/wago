// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "fmt"

// ownedMemoryReservationPages bounds the physical reservation independently of
// the exact declared maximum used for linking, metadata, and resource policy.
// Apply this before converting page counts to host-sized byte lengths.
func ownedMemoryReservationPages(def memoryDef) (uint64, error) {
	pages := uint64(65536)
	if def.Addr64 {
		pages = 65535
	}
	if def.HasMax && def.Max < pages {
		pages = def.Max
	}
	if def.Min > pages {
		return 0, fmt.Errorf("memory minimum %d exceeds finite reservation %d pages", def.Min, pages)
	}
	return pages, nil
}
