// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func saturatingFloatKind(kind wasm.InstrKind) (byte, bool) {
	for i, k := range [...]wasm.InstrKind{wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U, wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U, wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U, wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U} {
		if kind == k {
			return byte(i), true
		}
	}
	return 0, false
}

// RISC-V conversion already clips overflow to the integer bounds. Replace
// its NaN result with zero for both scalar and SIMD saturating conversions.
func (f *integerEmitter) floatTruncSaturating(wide, resultWide, unsigned bool) {
	f.floatInput(enc.F0, enc.X5, wide)
	nan := f.floatNaNBranch(wide)
	f.floatTruncRTZ(wide, resultWide, unsigned)
	end := f.Jump()
	f.floatPatch(nan)
	f.Constant(5, 0, resultWide)
	f.floatPatch(end)
	if !resultWide {
		f.a.ZeroExtend32(enc.X5, enc.X5)
	}
}
