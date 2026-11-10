// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"syscall"
	"testing"
	"unsafe"
)

func globalCleanupCompile(t *testing.T, module []byte, artifact bool) *Compiled {
	t.Helper()
	c, err := Compile(artifactTestConfig(artifact), module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	return c

}

func globalCleanupMappingStatus(address uintptr) syscall.Errno {
	var resident [1]byte
	_, _, errno := syscall.Syscall(syscall.SYS_MINCORE, address, uintptr(syscall.Getpagesize()), uintptr(unsafe.Pointer(&resident[0])))
	return errno
}
