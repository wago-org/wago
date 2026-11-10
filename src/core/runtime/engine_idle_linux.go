// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && (amd64 || arm64 || (riscv64 && !tinygo && !wago_target_tinygo))

package runtime

import "syscall"

const idleNativeStackHotBytes = 512 << 10

func (e *Engine) prepareIdleStackForCache() bool {
	if len(e.stack) > int(DefaultNativeStackBytes) {
		return false
	}
	// Round down so the discard cannot reach the retained top of the stack.
	page := syscall.Getpagesize()
	cold := (len(e.stack) - idleNativeStackHotBytes) &^ (page - 1)
	return madviseDontNeed(e.stack[:cold]) == nil
}
