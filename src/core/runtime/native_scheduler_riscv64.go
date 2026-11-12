// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import _ "unsafe"

//go:linkname nativeEnterSyscall runtime.entersyscall
func nativeEnterSyscall()

//go:linkname nativeExitSyscall runtime.exitsyscall
func nativeExitSyscall()

//go:nosplit
func enterNative(code, serArgs, linMem, trap, results, stack uintptr) {
	nativeEnterSyscall()
	enterNativeRaw(code, serArgs, linMem, trap, results, stack)
	nativeExitSyscall()
}

// Only a compiler-proven bounded wrapper may retain the Go P.
//
//go:nosplit
func enterNativeBounded(code, serArgs, linMem, trap, results, stack uintptr) {
	enterNativeRaw(code, serArgs, linMem, trap, results, stack)
}
