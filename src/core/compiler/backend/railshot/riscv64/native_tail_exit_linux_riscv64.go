// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import coreruntime "github.com/wago-org/wago/src/core/runtime"

// One process-lifetime exit identity keeps alternating instance tails flat.
var nativeTailExitState nativeTailExitCache

func nativeTailExitPointer() (uintptr, error) {
	return nativeTailExitState.get(func() ([]byte, error) { return tailRAExitCodeInvocation(true) }, coreruntime.NewCodeBuffer, (*coreruntime.CodeBuffer).Seal, (*coreruntime.CodeBuffer).Close)
}
