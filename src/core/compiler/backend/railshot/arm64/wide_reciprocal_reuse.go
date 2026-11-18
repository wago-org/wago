// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var wideReciprocalReuseEnabled = os.Getenv("WAGO_ARM64_NO_WIDE_RECIPROCAL_REUSE") != "1"

func (f *fn) wideReciprocalReuseActive() bool {
	return wideReciprocalReuseEnabled && f.wideReciprocalActive && !f.makesCalls && !f.moduleEH && len(f.customInstructions) == 0 && !f.streamingReduction && f.selectGroupEnd == 0 && f.localBase == 0 && f.inlineForwardN == 0
}

// The returned register obeys the ordinary owned-scratch contract. A remembered
// reciprocal is never reserved or kept alive after its consumer.
func (f *fn) readWideReciprocal(value uint64, avoid regMask) (Reg, bool) {
	st := storage{kind: stConst, typ: mtI64, cval: int64(value)}
	if reg, ok := f.cachedIntConst(st); ok {
		return reg, false
	}
	if reg, ok := f.reuseWideReciprocal(value, avoid); ok {
		return reg, true
	}
	reg, owned := f.intConstReadReg(st, avoid)
	if owned && f.wideReciprocalReuseActive() {
		f.wideReciprocalBits, f.wideReciprocalEnd, f.wideReciprocalReg = value, uint32(f.a.Len()), reg
	}
	return reg, owned
}

// Source control boundaries clear the fact even when they emit no native word.
// Check every intervening native write too, including UMULH and partial W writes.
func (f *fn) reuseWideReciprocal(value uint64, avoid regMask) (Reg, bool) {
	if !f.wideReciprocalReuseActive() || f.wideReciprocalEnd == 0 || f.wideReciprocalBits != value {
		return regNone, false
	}
	reg := f.wideReciprocalReg
	if reg >= 31 || avoid.has(reg) || f.pinned.has(reg) || f.reserved.has(reg) || f.pinnedLocalMask.has(reg) || f.regUser[reg] != nil {
		return regNone, false
	}
	start, end := int(f.wideReciprocalEnd), f.a.Len()
	if end < start || end-start > 48 || (end-start)&3 != 0 {
		f.wideReciprocalEnd = 0
		return regNone, false
	}
	for pc := start; pc < end; pc += 4 {
		effects, ok := nativeLoopConstWord(rdWord(f.a.B, pc))
		if !ok || effects.writes&(uint32(1)<<reg) != 0 {
			f.wideReciprocalEnd = 0
			return regNone, false
		}
	}
	f.wideReciprocalEnd = uint32(end)
	f.stats.peep("wide-reciprocal-reuse")
	return reg, true
}
