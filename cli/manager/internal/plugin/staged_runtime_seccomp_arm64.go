// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && arm64

package plugin

import "golang.org/x/sys/unix"

func stagedRuntimeSeccompArchitecture() (uint32, []uint32, []uint32, bool) {
	return unix.AUDIT_ARCH_AARCH64, nil, []uint32{unix.SYS_CLONE3}, true
}
