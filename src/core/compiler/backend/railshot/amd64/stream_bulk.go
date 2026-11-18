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
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// streamBulkRead32 reads an already materialized operand without changing its
// ownership. Fixed destination registers were evicted together, so sequential
// copies cannot destroy another argument or a value live after this operation.
func (s *streamState) streamBulkRead32(dst Reg, id streamID) {
	op := s.operand(id)
	switch op.Kind {
	case shared.ScalarConstant:
		s.target.a.MovImm32(dst, int32(op.Constant))
	case shared.ScalarRegister:
		s.target.a.MovRegReg32(dst, Reg(op.Reg))
	case shared.ScalarFrame:
		s.target.a.Load32(dst, RSP, op.Offset)
	default:
		panic("stream: deferred bulk operand")
	}
}

// bulk supports only memory.copy/fill on the admitted unshared memory32 zero.
// All range checks finish before the first write, including zero-length ranges.
// No legacy operand stack, register allocator or local-state flush is involved.
func (s *streamState) bulk(r *wasm.Reader, sub uint32) error {
	if sub != 10 && sub != 11 {
		return fmt.Errorf("stream: unsupported bulk opcode %d", sub)
	}
	dst, err := r.U32()
	if err != nil {
		return err
	}
	src := uint32(0)
	if sub == 10 {
		src, err = r.U32()
		if err != nil {
			return err
		}
	}
	f := s.target
	if dst != 0 || src != 0 || f.memoryAddr64(0) || f.threadedMemory0 {
		return fmt.Errorf("stream: unsupported bulk memory")
	}
	count, value, dest := s.pop(), s.pop(), s.pop()
	// Resolve pure arithmetic before fixing the machine registers. Any source
	// registers subsequently evicted have valid homes read by streamBulkRead32.
	s.materialize(dest, 0)
	s.materialize(value, 0)
	s.materialize(count, 0)
	mask := uint64(1)<<RAX | uint64(1)<<RCX | uint64(1)<<RDX | uint64(1)<<RSI | uint64(1)<<RDI | uint64(1)<<R8
	s.evict(mask)
	s.streamBulkRead32(RDI, dest)
	if sub == 10 {
		s.streamBulkRead32(RSI, value)
	} else {
		s.streamBulkRead32(RAX, value)
	}
	s.streamBulkRead32(RCX, count)
	// The uint32 addends fit in 64 bits, so neither end can silently wrap.
	f.a.LeaScaled(RDX, RDI, RCX, 0, 0)
	f.a.AluRM(cmpRMcode, RDX, RBX, -bdCurBytes, true)
	f.trapIf(condA, trapMemOOB)
	if sub == 10 {
		f.a.LeaScaled(RDX, RSI, RCX, 0, 0)
		f.a.AluRM(cmpRMcode, RDX, RBX, -bdCurBytes, true)
		f.trapIf(condA, trapMemOOB)
		f.a.Add64(RSI, RBX)
	}
	f.a.Add64(RDI, RBX)
	// The initial stream engine admits integer values only: XMM0..3 contain no
	// live stream values. Keep this restriction until a second-bank owner exists.
	if sub == 10 {
		f.emitCheckedMemoryCopy([4]Reg{0, 1, 2, 3})
	} else {
		f.emitCheckedMemoryFill(0)
	}
	s.release(dest)
	s.release(value)
	s.release(count)
	return nil
}
