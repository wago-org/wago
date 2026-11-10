// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo

package riscv64

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

func flushICache(mem []byte, used int) error {
	if used < 1 || used > len(mem) {
		return fmt.Errorf("rv64: invalid cache synchronization range")
	}
	start := pointer(mem)
	if uintptr(used) > ^uintptr(0)-start {
		return fmt.Errorf("rv64: cache range overflows")
	}
	// flags=0 requests synchronization for the entire process, including hart
	// migration. A local FENCE.I or SYS_RISCV_FLUSH_ICACHE_LOCAL is insufficient.
	_, _, errno := syscall.Syscall(unix.SYS_RISCV_FLUSH_ICACHE, start, start+uintptr(used), 0)
	if errno != 0 {
		return errno
	}
	return nil
}
