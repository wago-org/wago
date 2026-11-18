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

func streamUnaryShape(op byte) (inputWide, resultWide, ok bool) {
	switch op {
	case 0x45, 0x67, 0x68, 0x69, 0xc0, 0xc1:
		return false, false, true
	case 0x50, 0xa7:
		return true, false, true
	case 0x79, 0x7a, 0x7b, 0xc2, 0xc3, 0xc4:
		return true, true, true
	case 0xac, 0xad:
		return false, true, true
	}
	return false, false, false
}

// integerResult transfers consumed stack references only after native reads
// finish. An aliased input keeps its owner and must not be the result register.
func (s *streamState) integerResult(reg uint8, wide bool, inputs ...streamID) {
	for _, id := range inputs {
		s.release(id)
	}
	id := s.add(streamNode{kind: shared.ScalarRegister, reg: reg, wide: wide, refs: 1})
	s.owners[reg] = id
	s.stack = append(s.stack, id)
}

func (s *streamState) integerUnary(op byte) error {
	inputWide, resultWide, ok := streamUnaryShape(op)
	if !ok {
		return fmt.Errorf("stream: unsupported integer unary %#x", op)
	}
	id := s.pop()
	src := s.materialize(id, 0)
	dst := src
	if s.node(id).refs != 1 {
		dst = s.alloc(uint64(1) << src)
	}
	f, a := s.target, s.target.a
	// The result is about to diverge from any retained immutable generation.
	s.forgetRegisterBacking(dst)
	switch op {
	case 0x45, 0x50:
		a.TestSelf(Reg(src), inputWide)
		a.SetccReg(condE, Reg(dst))
	case 0xa7, 0xad:
		// Both truncate and unsigned extension must discard incoming i32 ABI dirt.
		a.MovRegReg32(Reg(dst), Reg(src))
	case 0xac, 0xc4:
		a.Movsxd(Reg(dst), Reg(src))
	case 0xc0, 0xc2:
		a.Movsx8(Reg(dst), Reg(src), resultWide)
	case 0xc1, 0xc3:
		a.Movsx16(Reg(dst), Reg(src), resultWide)
	case 0x67, 0x79:
		if a.BitCountState&shared.BitCountLZCNT != 0 {
			a.Lzcnt(Reg(dst), Reg(src), inputWide)
			a.BitCountState |= shared.BitCountLZCNT << 4
		} else {
			a.Bsr(Reg(dst), Reg(src), inputWide)
			zero := a.JccPlaceholder(condE)
			width := int32(32)
			if inputWide {
				width = 64
			}
			a.AluRI(6, Reg(dst), width-1, false)
			end := a.JmpPlaceholder()
			a.PatchRel32(zero, a.Len())
			a.MovImm32(Reg(dst), width)
			a.PatchRel32(end, a.Len())
		}
	case 0x68, 0x7a:
		if a.BitCountState&shared.BitCountTZCNT != 0 {
			a.Tzcnt(Reg(dst), Reg(src), inputWide)
			a.BitCountState |= shared.BitCountTZCNT << 4
		} else {
			a.Bsf(Reg(dst), Reg(src), inputWide)
			nonzero := a.JccPlaceholder(condNE)
			width := int32(32)
			if inputWide {
				width = 64
			}
			a.MovImm32(Reg(dst), width)
			a.PatchRel32(nonzero, a.Len())
		}
	case 0x69, 0x7b:
		if a.BitCountState&shared.BitCountPOPCNT != 0 {
			a.Popcnt(Reg(dst), Reg(src), inputWide)
			a.BitCountState |= shared.BitCountPOPCNT << 4
		} else {
			// Allocate every scratch before emitting: alloc may spill a live source.
			avoid := uint64(1)<<src | uint64(1)<<dst
			tmp := s.alloc(avoid)
			mask := uint8(0)
			if inputWide {
				mask = s.alloc(avoid | uint64(1)<<tmp)
			}
			f.Move(dst, src, inputWide)
			f.swarShift(Reg(tmp), Reg(dst), 1, inputWide)
			f.swarAnd(Reg(tmp), Reg(mask), 0x5555555555555555, inputWide, true)
			a.AluRR(0x29, Reg(dst), Reg(tmp), inputWide)
			f.swarShift(Reg(tmp), Reg(dst), 2, inputWide)
			f.swarAnd(Reg(dst), Reg(mask), 0x3333333333333333, inputWide, true)
			f.swarAnd(Reg(tmp), Reg(mask), 0x3333333333333333, inputWide, false)
			a.AluRR(0x01, Reg(dst), Reg(tmp), inputWide)
			f.swarShift(Reg(tmp), Reg(dst), 4, inputWide)
			a.AluRR(0x01, Reg(dst), Reg(tmp), inputWide)
			f.swarAnd(Reg(dst), Reg(mask), 0x0f0f0f0f0f0f0f0f, inputWide, true)
			if inputWide {
				a.MovImm64(Reg(mask), 0x0101010101010101)
				a.IMul(Reg(dst), Reg(mask), true)
				a.ShiftImm(5, Reg(dst), 56, true)
			} else {
				a.ImulRI(Reg(dst), 0x01010101, false)
				a.ShiftImm(5, Reg(dst), 24, false)
			}
		}
	}
	s.integerResult(dst, resultWide, id)
	return nil
}

func (s *streamState) integerSelect() error {
	cond, right, left := s.pop(), s.pop(), s.pop()
	wide := s.node(left).wide
	// Resolve deferred fixed-register operations before caching any operand
	// register: a shift in another operand may evict an earlier RCX owner.
	s.materialize(cond, 0)
	s.materialize(right, 0)
	s.materialize(left, 0)
	saved := s.reserved
	cr := s.materialize(cond, 0)
	s.reserved |= uint64(1) << cr
	rr := s.materialize(right, 0)
	s.reserved |= uint64(1) << rr
	lr := s.materialize(left, 0)
	s.reserved |= uint64(1) << lr
	dst := s.alloc(0)
	a := s.target.a
	s.forgetRegisterBacking(dst)
	s.target.Move(dst, rr, wide)
	a.TestSelf(Reg(cr), false)
	a.Cmovcc(condNE, Reg(dst), Reg(lr), wide)
	// Cmovcc32's untaken arm retains the prior normalized destination.
	s.reserved = saved
	s.integerResult(dst, wide, cond, right, left)
	return nil
}

func streamRotateShape(op byte) (wide bool, ok bool) {
	switch op {
	case 0x77, 0x78:
		return false, true
	case 0x89, 0x8a:
		return true, true
	}
	return false, false
}

func (s *streamState) integerRotate(op byte) error {
	wide, ok := streamRotateShape(op)
	if !ok {
		return fmt.Errorf("stream: unsupported rotate %#x", op)
	}
	// Inspect before materialization erases the constant tag. Immediate rotates
	// neither reserve nor evict RCX, including when its lease is persistent.
	countID := s.stack[len(s.stack)-1]
	if count := *s.node(countID); count.kind == shared.ScalarConstant {
		mask := uint64(31)
		if wide {
			mask = 63
		}
		amount := byte(uint64(count.constant) & mask)
		s.pop()
		value := s.pop()
		vr := s.materialize(value, 0)
		dst := vr
		if s.node(value).refs != 1 {
			dst = s.alloc(uint64(1) << vr)
		}
		// A same-register zero i64 rotate emits no write. Preserve its proof.
		if !wide || dst != vr || amount != 0 {
			s.forgetRegisterBacking(dst)
		}
		if wide {
			s.target.Move(dst, vr, true)
		} else if dst != vr || amount == 0 {
			// Nonzero rotate32 itself clears the upper half. A zero rotate is
			// elided by ShiftImm, so normalize its potentially dirty ABI input.
			s.target.a.MovRegReg32(Reg(dst), Reg(vr))
		}
		digit := byte(0)
		if op == 0x78 || op == 0x8a {
			digit = 1
		}
		s.target.a.ShiftImm(digit, Reg(dst), amount, wide)
		s.target.stats.peep("streaming-rotate-immediate")
		s.integerResult(dst, wide, countID, value)
		return nil
	}
	if s.reserved&(uint64(1)<<RCX) != 0 {
		return fmt.Errorf("stream: rotate count register reserved")
	}
	saved := s.reserved
	s.evict(uint64(1) << RCX)
	s.reserved |= uint64(1) << RCX
	count, value := s.pop(), s.pop()
	cr := s.materialize(count, uint64(1)<<RCX)
	s.reserved |= uint64(1) << cr
	vr := s.materialize(value, uint64(1)<<RCX)
	dst := vr
	if s.node(value).refs != 1 || value == count {
		dst = s.alloc(uint64(1) << vr)
	}
	a := s.target.a
	s.forgetRegisterBacking(dst)
	if wide {
		s.target.Move(dst, vr, true)
	} else {
		// A zero effective rotate count must still normalize an incoming i32.
		a.MovRegReg32(Reg(dst), Reg(vr))
	}
	a.MovRegReg32(RCX, Reg(cr))
	digit := byte(0)
	if op == 0x78 || op == 0x8a {
		digit = 1
	}
	a.ShiftCL(digit, Reg(dst), wide)
	s.reserved = saved
	s.integerResult(dst, wide, count, value)
	return nil
}
