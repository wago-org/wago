// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo

package riscv64

import _ "unsafe"

const nativeAvailable = true

//go:linkname enterSyscall runtime.entersyscall
func enterSyscall()

//go:linkname exitSyscall runtime.exitsyscall
func exitSyscall()

//go:noescape
func enterRaw(code, a0, a1, low, top, frame, trap uintptr) (result, status uint64)

//go:nosplit
func nativeCall(code, a0, a1, low, top, frame, trap uintptr) (uint64, uint64) {
	// Record a scannable Go stack and release the P before foreign entry. No
	// allocation or Go callback may occur until exitsyscall reacquires the P.
	enterSyscall()
	result, status := enterRaw(code, a0, a1, low, top, frame, trap)
	exitSyscall()
	return result, status
}
