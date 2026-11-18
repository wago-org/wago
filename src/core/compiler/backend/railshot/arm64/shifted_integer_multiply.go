// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"math/bits"
	"os"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

var shiftedIntegerMultiplyEnabled = os.Getenv("WAGO_ARM64_NO_SHIFTED_INTEGER_MULTIPLY") != "1"

// ARM64 shifted arithmetic covers 1 +/- 2^k at the operation's wrapping width.
// Keep the existing 3/5/9 selection as the same-binary qualification control.
func shiftedIntegerMultiplyForm(value int64, wide bool) (shift uint8, subtract, ok bool) {
	switch value {
	case 3:
		return 1, false, true
	case 5:
		return 2, false, true
	case 9:
		return 3, false, true
	}
	if !shiftedIntegerMultiplyEnabled {
		return 0, false, false
	}
	c := uint64(value)
	if !wide {
		c = uint64(uint32(c))
	}
	plus, minus := c-1, uint64(1)-c
	if !wide {
		plus = uint64(uint32(plus))
		minus = uint64(uint32(minus))
	}
	if plus != 0 && plus&(plus-1) == 0 {
		return uint8(bits.TrailingZeros64(plus)), false, true
	}
	if minus != 0 && minus&(minus-1) == 0 {
		return uint8(bits.TrailingZeros64(minus)), true, true
	}
	return 0, false, false
}

func (f *fn) shiftedIntegerMultiply(dst, src Reg, shift uint8, subtract, wide bool) {
	if subtract {
		f.a.SubShiftedReg(dst, src, src, a64.RegShiftLSL, shift, !wide)
	} else {
		f.a.AddShifted(dst, src, src, shift, !wide)
	}
}

// A cached multiplier already costs no materialization. Keep its MUL unless
// the coefficient belongs to the established short-shift forms: replacing it
// with a longer shifted ADD can contend with the loop's other integer work.
func (f *fn) selectShiftedIntegerMultiply(st storage, wide bool) (shift uint8, subtract, ok bool) {
	if st.cval != 3 && st.cval != 5 && st.cval != 9 {
		if _, cached := f.cachedIntConst(st); cached {
			return 0, false, false
		}
	}
	return shiftedIntegerMultiplyForm(st.cval, wide)
}
