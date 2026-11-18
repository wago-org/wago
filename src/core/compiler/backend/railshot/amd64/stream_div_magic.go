// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"os"
)

var streamingMagic32Enabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_MAGIC32") == "1"

// integerDivMagic32 preserves the legacy unsigned constant-divisor policy
// without importing operand trees. Powers of two and trapping zero divisors
// retain their existing paths. A false return leaves state and code unchanged.
func (s *streamState) integerDivMagic32(op byte) bool {
	if !streamingMagic32Enabled || (op != 0x6e && op != 0x70) || len(s.stack) < 2 {
		return false
	}
	right, left := s.stack[len(s.stack)-1], s.stack[len(s.stack)-2]
	divisor := *s.node(right)
	if divisor.kind != shared.ScalarConstant {
		return false
	}
	d := uint32(divisor.constant)
	if d == 0 || d&(d-1) == 0 {
		return false
	}
	magic, shift, add := railshot.MagicU(uint64(d), 32)
	const fixed = uint64(1)<<RAX | uint64(1)<<RDX
	f, a := s.target, s.target.a
	if add && (s.reserved|uint64(f.reserved.union(f.pinned).union(f.pinnedLocalMask)))&fixed != 0 {
		return false
	}
	// Finish deferred operands before taking the fixed pair. Re-read their
	// locations afterwards: evacuation may replace a register with a frame.
	s.materialize(left, 0)
	saved := s.reserved
	if add {
		s.evict(fixed)
		s.reserved |= fixed
	}
	src := s.materialize(left, 0)
	s.reserved |= uint64(1) << src
	q := s.alloc(uint64(1) << src)
	s.reserved |= uint64(1) << q
	s.forgetRegisterBacking(q)
	if add {
		// n is normalized even when its native carrier has dirty upper bits.
		a.MovRegReg32(RAX, Reg(src))
		a.MovImm64(Reg(q), ^uint64(0)/uint64(d)+1)
		a.Mul(Reg(q), true)
		a.MovRegReg32(Reg(q), RDX)
		f.stats.peep("streaming-magic-wide-u32")
	} else {
		multiplier := s.alloc(uint64(1)<<src | uint64(1)<<q)
		s.forgetRegisterBacking(multiplier)
		a.MovRegReg32(Reg(q), Reg(src))
		a.MovImm32(Reg(multiplier), int32(uint32(magic)))
		a.IMul(Reg(q), Reg(multiplier), true)
		a.ShiftImm(5, Reg(q), 32+byte(shift), true)
		f.stats.peep("streaming-magic-product-shift")
	}
	if op == 0x70 {
		a.ImulRI(Reg(q), int32(d), false)
		a.Neg(Reg(q), false)
		a.AluRR(0x01, Reg(q), Reg(src), false)
	}
	s.reserved = saved
	s.stack = s.stack[:len(s.stack)-2]
	s.integerResult(q, false, left, right)
	return true
}
