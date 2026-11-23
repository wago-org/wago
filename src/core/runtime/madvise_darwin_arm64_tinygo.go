// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build darwin && arm64 && tinygo

package runtime

import "unsafe"

//export madvise
func libcMadvise(address unsafe.Pointer, length uintptr, advice int32) int32

func madviseDontNeed(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	const madvZero = 11
	if libcMadvise(unsafe.Pointer(unsafe.SliceData(b)), uintptr(len(b)), madvZero) != 0 {
		// Preserve zero-on-reuse when Darwin rejects MADV_ZERO after a fork.
		clear(b)
	}
	return nil
}
