// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows && amd64 && !tinygo

package runtime

import "unsafe"

// The prepared numeric owner uses these static ABIInternal entries. The
// generic bounded callers retain their ordinary Windows fallback.
//
//go:noescape
func inlineHostContextEnter(fn FixedScalarHostContextCall, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64

//go:noescape
func inlineHostViewContextEnter(fn FixedHostContextCallView, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64
