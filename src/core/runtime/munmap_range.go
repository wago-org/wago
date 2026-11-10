// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux && (amd64 || arm64)) || (darwin && (amd64 || arm64) && !tinygo) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package runtime

import "syscall"

func munmapRange(base, length uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_MUNMAP, base, length, 0); errno != 0 {
		return errno
	}
	return nil
}
