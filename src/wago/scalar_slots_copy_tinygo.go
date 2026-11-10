// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo

package wago

func copyNarrowScalarSlots(dst, src []uint64, n int) {
	dst, src = dst[:n], src[:n]
	for i := 0; i < n; i++ {
		dst[i] = uint64(uint32(src[i]))
	}
}
