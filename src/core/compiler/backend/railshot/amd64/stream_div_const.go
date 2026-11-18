// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"math/bits"
)

// integerDivConst is a preflighted, stream-owned power-of-two reduction. Call
// before materializing the divisor (which would erase its constant tag) and
// before reserving the generic IDIV registers. False leaves state/code intact;
// true consumes dividend/divisor and pushes one result. It is eager, including
// the signed /-1 overflow trap, even when a later instruction drops the result.
func (s *streamState) integerDivConst(op byte) bool {
	wide, signed, remainder, ok := streamDivShape(op)
	if !ok || len(s.stack) < 2 {
		return false
	}
	right, left := s.stack[len(s.stack)-1], s.stack[len(s.stack)-2]
	divisor := *s.node(right)
	if divisor.kind != shared.ScalarConstant {
		return false
	}
	d := divisor.constant
	if !wide {
		if signed {
			d = int64(int32(d))
		} else {
			d = int64(uint32(d))
		}
	}
	magnitude := uint64(d)
	negative := signed && d < 0
	if negative {
		magnitude = 0 - magnitude
	} // Includes the signed minimum divisor.
	if magnitude == 0 {
		return false
	}
	if magnitude&(magnitude-1) != 0 {
		return s.integerDivMagic32(op)
	}
	s.stack = s.stack[:len(s.stack)-2]
	src := s.materialize(left, 0)
	dst := src
	if s.node(left).refs != 1 {
		dst = s.alloc(uint64(1) << src)
	}
	f, a := s.target, s.target.a
	// Allocate all native scratch before modifying any input. Explicit masks
	// protect ownerless result/scratch registers; no legacy allocator is used.
	avoid := uint64(1)<<src | uint64(1)<<dst
	tmp, mask := uint8(0), uint8(0)
	needsBias := signed && magnitude > 1
	if needsBias || signed && d == -1 && !remainder && wide {
		tmp = s.alloc(avoid)
		avoid |= uint64(1) << tmp
	}
	needsMask := remainder && magnitude > 1 && wide && magnitude-1 > 0x7fffffff
	if needsMask {
		mask = s.alloc(avoid)
	}
	// Same-register i64 division by +1 is the only no-write case here.
	if !wide || dst != src || remainder || d != 1 {
		s.forgetRegisterBacking(dst)
	}
	if wide {
		f.Move(dst, src, true)
	} else {
		a.MovRegReg32(Reg(dst), Reg(src))
	}
	switch {
	case magnitude == 1 && remainder:
		a.XorSelf32(Reg(dst))
	case signed && d == -1:
		if wide {
			a.MovImm64(Reg(tmp), 0x8000000000000000)
			a.AluRR(0x39, Reg(dst), Reg(tmp), true)
		} else {
			a.AluRI(7, Reg(dst), -2147483648, false)
		}
		f.trapIf(condE, trapDivOverflow)
		a.Neg(Reg(dst), wide)
	case magnitude == 1:
		// The normalized dividend is the quotient.
	default:
		k := byte(bits.TrailingZeros64(magnitude))
		if signed {
			width := byte(32)
			if wide {
				width = 64
			}
			f.Move(tmp, dst, wide)
			a.ShiftImm(7, Reg(tmp), width-1, wide)
			a.ShiftImm(5, Reg(tmp), width-k, wide)
			a.AluRR(0x01, Reg(dst), Reg(tmp), wide)
		}
		if remainder {
			if needsMask {
				a.MovImm64(Reg(mask), magnitude-1)
				a.AluRR(0x21, Reg(dst), Reg(mask), true)
			} else {
				a.AluRI(4, Reg(dst), int32(magnitude-1), wide)
			}
			if signed {
				a.AluRR(0x29, Reg(dst), Reg(tmp), wide)
			}
		} else if signed {
			a.ShiftImm(7, Reg(dst), k, wide)
			if negative {
				a.Neg(Reg(dst), wide)
			}
		} else {
			a.ShiftImm(5, Reg(dst), k, wide)
		}
	}
	f.stats.peep("streaming-div-pow2")
	s.integerResult(dst, wide, left, right)
	return true
}
