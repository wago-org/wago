// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && (amd64 || arm64) && ((windows && arm64) || tinygo)

package runtime

import "unsafe"

func integerHostContextBridge(view bool) uintptr { return 0 }
func inlineHostIntegerContextEnter(fn FixedScalarHostContextCall, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64 {
	panic("integer guest context unavailable")
}
func inlineHostIntegerViewContextEnter(fn FixedHostContextCallView, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64 {
	panic("integer guest context unavailable")
}

func inlineHostIntegerI32Enter(fn func(int32) int32, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64 {
	panic("integer i32 context unavailable")
}
