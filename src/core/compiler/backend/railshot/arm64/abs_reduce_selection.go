// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var absReduceSelectionEnabled = os.Getenv("WAGO_ARM64_NO_ABS_REDUCE_SELECTION") != "1"

// Biases belong to the dynamic wrapping i32 address, before the widened
// range endpoint. Keep zero canonicalization and the original large-bias path.
func (f *fn) absReduceOffset(dst Reg, bias uint32) {
	if absReduceSelectionEnabled && bias <= 4095 {
		if bias == 0 {
			f.a.MovReg32(dst, dst)
		} else {
			f.a.AddImm32(dst, dst, bias)
		}
		return
	}
	f.a.MovImm64(X16, uint64(bias))
	f.a.Add32(dst, dst, X16)
}
