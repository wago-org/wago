// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && !tinygo

package wago

//go:noescape
func copyNarrowScalarSlotsSIMD(dst, src *uint64, n uintptr)

func copyNarrowScalarSlots(dst, src []uint64, n int) {
	dst, src = dst[:n], src[:n]
	if n < 4 {
		for i := range src {
			dst[i] = uint64(uint32(src[i]))
		}
		return
	}
	copyNarrowScalarSlotsSIMD(&dst[0], &src[0], uintptr(n))
}
