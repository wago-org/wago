// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64 && !tinygo

package runtime

//go:nosplit
func enterNativeIntLight(code, linMem, a0, a1, a2, a3, stack uintptr) uintptr {
	nativeEnterSyscall()
	result := enterNativeIntLightRaw(code, linMem, a0, a1, a2, a3, stack)
	nativeExitSyscall()
	return result
}
