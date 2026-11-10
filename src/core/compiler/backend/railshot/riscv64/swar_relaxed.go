// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

// Baseline relaxed lowering makes one fixed deterministic choice, using the
// corresponding qualified core operation. These choices are the R=0 behavior
// in the WebAssembly deterministic profile: strict swizzle/saturation/minmax,
// per-bit lane selection, and saturation of the sole Q15 overflow.
// https://webassembly.github.io/spec/core/exec/numerics.html#relaxed-operations
func swarRelaxedCoreOpcode(kind uint32) (uint32, bool) {
	switch kind {
	case 256:
		return 14, true
	case 257:
		return 248, true
	case 258:
		return 249, true
	case 259:
		return 252, true
	case 260:
		return 253, true
	case 265, 266, 267, 268:
		return 82, true
	case 269:
		return 232, true
	case 270:
		return 233, true
	case 271:
		return 244, true
	case 272:
		return 245, true
	case 273:
		return 130, true
	default:
		return 0, false
	}
}
