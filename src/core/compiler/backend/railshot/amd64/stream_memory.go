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
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var streamingCachedBoundEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_CACHED_BOUND") == "1"

// streamCompareMemoryEnd preserves the explicit unsigned check. Shared memory's
// cache can hold a base pointer rather than an extent, so it must not be used.
func (f *fn) streamCompareMemoryEnd(end Reg) {
	if streamingCachedBoundEnabled && !f.threadedMemory0 && f.memSizeReg != regNone {
		f.a.Cmp64(end, f.memSizeReg)
		f.stats.peep("stream-cached-bound")
	} else {
		f.a.AluRM(cmpRMcode, end, RBX, -bdCurBytes, true)
	}
}

// streamMemoryShape rejects floating-point and proposal instructions before
// lowering. Alignment is a hint, not a requirement for plain scalar accesses.
func streamMemoryShape(op byte) (size int, signed, wide, store bool) {
	switch op {
	case 0x28, 0x2d, 0x2f:
	case 0x2c, 0x2e:
		signed = true
	case 0x29, 0x31, 0x33, 0x35:
		wide = true
	case 0x30, 0x32, 0x34:
		signed, wide = true, true
	case 0x36, 0x3a, 0x3b:
		store = true
	case 0x37, 0x3c, 0x3d, 0x3e:
		store, wide = true, true
	default:
		return 0, false, false, false
	}
	return memAccessSize(op), signed, wide, store
}

// streamCheckedAddress consumes a disposable zero-extended memory32 address
// and a distinct disposable scratch register. No legacy allocator or operand
// node is consulted. The returned displacement is safe for x86 sign extension.
//
// Arithmetic stays 64-bit: max(uint32 address)+max(uint32 offset)+8 fits well
// below 2^64. An overflowing memory32 effective address must trap, not wrap.
func (f *fn) streamCheckedAddress(addr, end Reg, off uint32, size int) int32 {
	return f.streamMemoryAddress(addr, end, off, size, true)
}

// Even a certified access retains the identical nonwrapping effective-address
// arithmetic. A hit removes only the comparison and its trap branch.
func (f *fn) streamMemoryAddress(addr, end Reg, off uint32, size int, check bool) int32 {
	disp := int32(0)
	if uint64(off)+uint64(size) <= 0x7fffffff {
		disp = int32(off)
		f.a.LeaDisp(end, addr, int32(off)+int32(size))
	} else {
		f.a.MovImm32(end, int32(off))
		f.a.Add64(addr, end)
		f.a.LeaDisp(end, addr, int32(size))
	}
	// Keep explicit checks even for guard mappings: one bounds failure precedes
	// any load/store, and stores cannot partially modify memory before trapping.
	if check {
		f.streamCompareMemoryEnd(end)
		f.trapIf(condA, trapMemOOB)
	}
	return disp
}

// memory eagerly executes accesses in bytecode order, including dropped loads.
// Deferred arithmetic is nontrapping in this engine; no pending memory access
// can be reordered across a store or attributed to a later instruction's PC.
func (s *streamState) memory(r *wasm.Reader, op byte) error {
	size, signed, wide, store := streamMemoryShape(op)
	if size == 0 {
		return fmt.Errorf("stream: unsupported memory opcode %#x", op)
	}
	f := s.target
	index, off, err := f.readMemArg(r)
	if err != nil {
		return err
	}
	if index != 0 || off > 0xffffffff || f.memoryAddr64(0) || f.threadedMemory0 {
		return fmt.Errorf("stream: unsupported memory address mode")
	}
	savedReserved := s.reserved
	var value streamID
	var valueReg uint8
	if store {
		value = s.pop()
	}
	address := s.pop()
	addressOrder := s.node(address).order
	extent := uint64(off) + uint64(size)
	covered := s.boundsCover(address, extent)
	if store {
		// A deferred address shift can evict RCX even when reserved: fixed
		// machine clobbers preserve IDs, not cached physical registers. Resolve
		// it before caching the value register, then refresh the address after
		// value materialization, which may itself spill the address snapshot.
		s.materialize(address, 0)
		valueReg = s.materialize(value, 0)
		s.reserved |= uint64(1) << valueReg
	}
	source := s.materialize(address, 0)
	ea := s.alloc(uint64(1) << source)
	f.a.MovRegReg32(Reg(ea), Reg(source))
	s.reserved |= uint64(1) << ea
	s.release(address)
	end := s.alloc(0)
	disp := f.streamMemoryAddress(Reg(ea), Reg(end), uint32(off), size, !covered)
	if covered {
		f.stats.peep("stream-bounds-fact")
	} else {
		s.rememberBounds(address, addressOrder, extent)
	}
	if store {
		f.a.StoreIdx(RBX, Reg(ea), Reg(valueReg), disp, size)
		s.release(value)
	} else {
		// Reuse the dead end-of-access scratch as the loaded value. Every i32
		// form writes a 32-bit destination, so upper ABI carrier bits are zero.
		f.a.LoadIdx(Reg(end), RBX, Reg(ea), disp, size, signed, wide)
		id := s.add(streamNode{kind: shared.ScalarRegister, reg: end, wide: wide, refs: 1})
		s.owners[end] = id
		s.stack = append(s.stack, id)
	}
	s.reserved = savedReserved
	return nil
}
