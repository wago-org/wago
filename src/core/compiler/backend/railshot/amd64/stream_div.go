// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func streamDivShape(op byte) (wide, signed, remainder, ok bool) {
	switch op {
	case 0x6d, 0x6e, 0x6f, 0x70:
		return false, op == 0x6d || op == 0x6f, op >= 0x6f, true
	case 0x7f, 0x80, 0x81, 0x82:
		return true, op == 0x7f || op == 0x81, op >= 0x81, true
	default:
		return false, false, false, false
	}
}

// integerDiv is deliberately eager: even a dropped quotient or remainder can
// trap, and its trap must retain this opcode's PC rather than a later consumer's.
// The stream allocator owns all mutable values; only native emission primitives
// and the common trap-site recorder are shared with the legacy compiler.
func (s *streamState) integerDiv(op byte) error {
	wide, signed, remainder, ok := streamDivShape(op)
	if !ok {
		return fmt.Errorf("stream: unsupported division opcode %#x", op)
	}
	if s.integerDivConst(op) {
		return nil
	}
	const fixed = uint64(1)<<RAX | uint64(1)<<RDX
	f := s.target
	if (s.reserved|uint64(f.reserved.union(f.pinned)))&fixed != 0 {
		return fmt.Errorf("stream: division scratch overlaps persistent GP lease")
	}
	right, left := s.pop(), s.pop()
	// Pure arithmetic operands are resolved before taking the fixed registers.
	// Either materialization may spill the other operand to its canonical home.
	s.materialize(left, 0)
	s.materialize(right, 0)
	s.evict(fixed)
	savedReserved := s.reserved
	s.reserved |= fixed
	// A divisor aliasing the dividend is fine, but neither fixed register may
	// carry it: CDQ/CQO and DIV overwrite the entire RDX:RAX pair.
	divisor := s.materialize(right, fixed)
	s.reserved |= uint64(1) << divisor
	dividend := s.operand(left)
	switch dividend.Kind {
	case shared.ScalarRegister:
		f.Move(uint8(RAX), dividend.Reg, wide)
	case shared.ScalarFrame:
		f.Load(uint8(RAX), dividend.Offset, wide)
	case shared.ScalarConstant:
		f.Constant(uint8(RAX), dividend.Constant, wide)
	default:
		panic("stream: deferred dividend after materialization")
	}
	f.a.TestSelf(Reg(divisor), wide)
	f.trapIf(condE, trapDivZero)
	switch {
	case signed && !remainder:
		f.a.AluRI(7, Reg(divisor), -1, wide)
		ordinary := f.a.JccPlaceholder(condNE)
		f.cmpIntMin(wide) // Only RDX is temporary; fixed pair is already evicted.
		f.trapIf(condE, trapDivOverflow)
		f.a.PatchRel32(ordinary, f.a.Len())
		f.a.Cdq(wide)
		f.a.Idiv(Reg(divisor), wide)
	case signed:
		// x % -1 is zero, including INT_MIN; IDIV would fault for that pair.
		f.a.AluRI(7, Reg(divisor), -1, wide)
		ordinary := f.a.JccPlaceholder(condNE)
		f.a.XorSelf32(RDX)
		done := f.a.JmpPlaceholder()
		f.a.PatchRel32(ordinary, f.a.Len())
		f.a.Cdq(wide)
		f.a.Idiv(Reg(divisor), wide)
		f.a.PatchRel32(done, f.a.Len())
	default:
		f.a.XorSelf32(RDX)
		f.a.Div(Reg(divisor), wide)
	}
	result := RAX
	if remainder {
		result = RDX
	}
	s.release(left)
	s.release(right)
	s.reserved = savedReserved
	id := s.add(streamNode{kind: shared.ScalarRegister, reg: uint8(result), wide: wide, refs: 1})
	s.owners[result] = id
	s.stack = append(s.stack, id)
	return nil
}
