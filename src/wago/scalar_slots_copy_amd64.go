// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

//go:noescape
func copyNarrowScalarSlotsAVX2(dst, src *uint64, n uintptr)

var narrowScalarAVX2Supported = detectNarrowScalarAVX2()

func detectNarrowScalarAVX2() bool {
	features, ok := cachedAMD64CPUFeatures()
	return ok && features.Has(shared.AMD64AVX|shared.AMD64AVX2)
}

func copyNarrowScalarSlots(dst, src []uint64, n int) {
	dst, src = dst[:n], src[:n]
	if n >= 16 && narrowScalarAVX2Supported {
		copyNarrowScalarSlotsAVX2(&dst[0], &src[0], uintptr(n))
		return
	}
	i := 0
	for ; i+4 <= n; i += 4 {
		_ = dst[i+3]
		_ = src[i+3]
		dst[i] = uint64(uint32(src[i]))
		dst[i+1] = uint64(uint32(src[i+1]))
		dst[i+2] = uint64(uint32(src[i+2]))
		dst[i+3] = uint64(uint32(src[i+3]))
	}
	for ; i < n; i++ {
		dst[i] = uint64(uint32(src[i]))
	}
}
