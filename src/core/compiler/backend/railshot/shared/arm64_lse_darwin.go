// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && darwin && !tinygo

package shared

import "golang.org/x/sys/unix"

var arm64LSEAvailable = func() bool {
	value, err := unix.SysctlUint32("hw.optional.arm.FEAT_LSE")
	if err != nil {
		value, err = unix.SysctlUint32("hw.optional.armv8_1_atomics")
	}
	return err == nil && value == 1
}()

// ARM64SupportsLSE reports native acquire/release single-instruction atomics.
func ARM64SupportsLSE() bool { return arm64LSEAvailable }
