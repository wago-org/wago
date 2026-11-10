// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_guardpage && linux && (amd64 || arm64 || riscv64)

package runtime

import (
	"syscall"
	"unsafe"
)

func growGuardedHostView(j *JobMemory, logicalBytes int) error {
	committed := len(j.mem) - j.linOff
	if logicalBytes <= committed {
		return nil
	}
	start := j.reserveBase + uintptr(j.linOff+committed)
	length := uintptr(logicalBytes - committed)
	if _, _, errno := syscall.Syscall(syscall.SYS_MPROTECT, start, length, syscall.PROT_READ|syscall.PROT_WRITE); errno != 0 {
		return errno
	}
	j.mem = unsafe.Slice(&j.mem[0], j.linOff+logicalBytes)
	return nil
}
