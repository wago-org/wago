// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var affineBoundsKeepEnds = os.Getenv("WAGO_ARM64_AFFINE_BOUNDS_KEEP_ENDS") == "1"

// A removed bound comparison need not make its ADD result dead. Track possible
// differences through this one straight-line loop, starting with all candidates
// dirty to cover the backedge. Every observed difference keeps that register's
// definitions. Remaining exit differences need a separate bounded continuation
// proof. No values or facts escape the selected plan.
func affineBoundsDeadEndRegs(b []byte, p *affineBoundsPlan, relocs []callReloc) uint32 {
	var candidates uint32
	for i := 0; i < p.omitN; i += 2 {
		candidates |= uint32(1) << uint32(rdWord(b, p.omit[i]-4)&31)
	}
	dirty, keep, omit := candidates, uint32(0), 0
	for pc := p.start; pc < p.back; pc += 4 {
		if omit < p.omitN && pc+4 == p.omit[omit] {
			dirty |= uint32(1) << uint32(rdWord(b, pc)&31)
			continue
		}
		if omit < p.omitN && pc == p.omit[omit] {
			omit++
			continue
		}
		x, ok := linearSpanExitWord(rdWord(b, pc))
		if !ok {
			return 0
		}
		keep |= x.reads & dirty
		dirty &^= x.writes
	}
	safe := candidates &^ keep
	for reg := Reg(0); reg < SP; reg++ {
		bit := uint32(1) << uint32(reg)
		if safe&dirty&bit != 0 && !spanExitClobbersDeadWith(b, p.back+4, bit, relocs, linearSpanExitWord) {
			safe &^= bit
		}
	}
	return safe
}
