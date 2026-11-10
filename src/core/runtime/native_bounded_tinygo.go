// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && (amd64 || arm64) && tinygo

package runtime

func enterNativeBounded(code, serArgs, linMem, trap, results, stack uintptr) {
	enterNative(code, serArgs, linMem, trap, results, stack)
}

func resumeNativeBounded(ctrl, stack uintptr) {
	resumeNative(ctrl, stack)
}
