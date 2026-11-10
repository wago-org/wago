// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo && ((linux && amd64) || ((linux || darwin) && arm64))

package runtime

import (
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

func markTinyGoTrampolineFailure(trap uintptr) {
	if trap == 0 {
		return
	}
	cell := (*uint32)(unsafe.Pointer(trap))
	for {
		old := atomic.LoadUint32(cell)
		if TrapCode(old) == TrapInterrupted || atomic.CompareAndSwapUint32(cell, old, uint32(TrapBuiltin)) {
			return
		}
	}
}

func markTinyGoResumeFailure(ctrl uintptr) {
	if ctrl == 0 {
		return
	}
	linMem := tinyGoSavedLinMem(ctrl)
	if linMem == 0 {
		return
	}
	trap := *(*uintptr)(unsafe.Pointer(linMem - abi.TrapCellPtrOffset))
	markTinyGoTrampolineFailure(trap)
}
