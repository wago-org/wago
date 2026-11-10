// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64 && !tinygo

package runtime

import goruntime "runtime"

//lint:ignore U1000 Used by inline_host_yield_arm64.h through go_asm.h.
const inlineHostYieldInterval = 1024

// The live assembly owner can loop through tiny Go callbacks without reaching
// a stack-growth check. Yield only after publishing the callback results, while
// the native continuation is parked and the owner's Go argument map is active.
//
//lint:ignore U1000 Called by ARM64 assembly through inline_host_yield_arm64.h.
func inlineHostYield() { goruntime.Gosched() }
