// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64 && tinygo

package runtime

import (
	"errors"
	"unsafe"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// The integer arguments arrive in their native registers X0-X3. TinyGo
// passes the linear memory in X4, stack top in X5, and code pointer in X6.
func tinygoARM64IntEntryCode(light bool) []byte {
	var a a64.Asm
	if light {
		a.SubImm64(a64.X10, a64.X5, 96)
		a.AddImm64(a64.X11, a64.SP, 0)
		a.StpOffset(a64.X11, a64.X26, a64.X10, 0)
		a.StpOffset(a64.FP, a64.LR, a64.X10, 16)
		// The light proof preserves callee-saved GPRs. Even an integer
		// signature can use FP temporaries, so preserve the AAPCS64 lanes.
		for reg, off := a64.Reg(8), int32(32); reg <= 14; reg, off = reg+2, off+16 {
			a.FStpOffset64(reg, reg+1, a64.X10, off)
		}
	} else {
		a.MovReg64(a64.X10, a64.X5)
		tinygoARM64SaveGoContext(&a, a64.X10)
	}
	a.MovReg64(a64.X26, a64.X4)
	a.AddImm64(a64.SP, a64.X10, 0)
	if !light {
		a.MovReg64(a64.X22, a64.ZR)
	}
	a.MovReg64(a64.FP, a64.ZR)
	a.Stur64(a64.X10, a64.X26, -offTrapStackReentry)
	continuation := a.Adr(a64.X11)
	a.Stur64(a64.X11, a64.X26, -arm64TrapHandlerPtrOffset)
	a.Blr(a64.X6)
	epilogue := a.Len()
	if !a.PatchAdr(continuation, epilogue) {
		panic("wago: arm64 TinyGo integer trampoline continuation is out of range")
	}
	if light {
		a.LdpOffset(a64.X11, a64.X26, a64.SP, 0)
		a.LdpOffset(a64.FP, a64.LR, a64.SP, 16)
		for reg, off := a64.Reg(8), int32(32); reg <= 14; reg, off = reg+2, off+16 {
			a.FLdpOffset64(reg, reg+1, a64.SP, off)
		}
		a.AddImm64(a64.SP, a64.X11, 0)
		a.Ret()
	} else {
		tinygoARM64RestoreGoContext(&a)
	}
	return a.B
}

func (e *Engine) EnterPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	entries := tinygoARM64Entries.Load()
	if entries == nil || e.stackTop == 0 {
		return 0, errors.New("tinygo prepared integer entry requires an open engine")
	}
	fv := tinygoARM64FuncValue{context: code, fnptr: entries.fullInt}
	call := *(*func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr)(unsafe.Pointer(&fv))
	return uint64(call(uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), linMemBase, e.stackTop)), nil
}

func (e *Engine) EnterPreparedIntLight(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return e.EnterPreparedInt(code, linMemBase, a0, a1, a2, a3)
}

func (e *Engine) EnterPreparedIntBounded(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return e.EnterPreparedInt(code, linMemBase, a0, a1, a2, a3)
}

func (e *Engine) EnterPreparedIntLightBounded(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return e.EnterPreparedInt(code, linMemBase, a0, a1, a2, a3)
}

func (e *Engine) PrepareIntCall(call *PreparedIntCall, code, linMem uintptr) {
	call.code, call.linMem, call.stack = code, linMem, e.stackTop
}

func (e *Engine) EnterPreparedIntCallBounded(call *PreparedIntCall, a0, a1, a2, a3 uint64) uint64 {
	result, _ := e.EnterPreparedIntLightBounded(call.code, call.linMem, a0, a1, a2, a3)
	return result
}

func (e *Engine) EnterPreparedIntPreboundContextBounded(call *PreparedIntCall, a0, a1, a2, a3 uint64) uint64 {
	entries := tinygoARM64Entries.Load()
	if entries == nil || e.stackTop == 0 {
		return 0
	}
	// Host re-entry can switch the active engine. Always pass its stack.
	fv := tinygoARM64FuncValue{context: call.code, fnptr: entries.lightInt}
	enter := *(*func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr)(unsafe.Pointer(&fv))
	return uint64(enter(uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), call.linMem, e.stackTop))
}

func PreparedIntTrapCode(trap []byte) TrapCode {
	if len(trap) < 4 {
		return TrapNone
	}
	return TrapCode(loadTrap(trap))
}

func ConsumePreparedIntTrap(trap []byte) error {
	tc := PreparedIntTrapCode(trap)
	if tc == TrapNone {
		return nil
	}
	storeTrap(trap, 0)
	return trapErrorFromBuffer(tc, trap)
}
