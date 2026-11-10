// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import "testing"

func TestCheckedHandleGenerationRetiresBeforeWrap(t *testing.T) {
	generations := []uint64{0, ^uint64(0)}
	c := Collector{handles: make([]handleEntry, 2), freeHandles: []uint32{1}, checkedHandles: &generations}
	h := c.newHandle(handleEntry{size: 16})
	if h != 2 || generations[1] != ^uint64(0) || generations[2] != 1 {
		t.Fatalf("handle=%d generations=%v", h, generations)
	}
}
