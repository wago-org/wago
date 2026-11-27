// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && tinygo

package runtime

import (
	"errors"
	"unsafe"

	"github.com/wago-org/wago/internal/buildfeatures"
)

func (e *Engine) PrepareIntCall(call *PreparedIntCall, code, linMem uintptr) {
	call.code, call.linMem, call.stack = code, linMem, e.stackTop
}

// PrepareBoundedIntContext is a no-op under TinyGo: its portable entry path
// establishes the trap re-entry context on each call.
func (*Engine) PrepareBoundedIntContext(uintptr) {}

func (e *Engine) EnterPreparedIntCallBounded(call *PreparedIntCall, a0, a1, a2, a3 uint64) uint64 {
	result, _ := e.EnterPreparedIntBounded(call.code, call.linMem, a0, a1, a2, a3)
	return result
}

// EnterPreparedIntPreboundContextBounded retains the immutable call descriptor
// contract while using TinyGo's portable per-call transition.
func (e *Engine) EnterPreparedIntPreboundContextBounded(call *PreparedIntCall, a0, a1, a2, a3 uint64) uint64 {
	result, _ := e.EnterPreparedIntBounded(call.code, call.linMem, a0, a1, a2, a3)
	return result
}

// TinyGo cannot assemble engine_int_amd64.s. Its indirect-call ABI passes the
// five explicit uintptr arguments in RDI, RSI, RDX, RCX, and R8, followed by the
// func-value context in R9. The generated thunk rearranges those registers into
// Wago's direct integer ABI and performs the same foreign-stack transition as
// the standard Go assembly entry.
const tinygoIntThunkPrologue = "\x49\xba\x00\x00\x00\x00\x00\x00\x00\x00" + // movabs stackTop, r10
	"\x49\x83\xea\x40" + // sub $64, r10
	"\x49\x89\x22" + // mov rsp, 0(r10)
	"\x49\x89\x6a\x08" + // mov rbp, 8(r10)
	"\x49\x89\x5a\x10" + // mov rbx, 16(r10)
	"\x4d\x89\x62\x18" + // mov r12, 24(r10)
	"\x4d\x89\x6a\x20" + // mov r13, 32(r10)
	"\x4d\x89\x72\x28" + // mov r14, 40(r10)
	"\x4d\x89\x7a\x30" + // mov r15, 48(r10)
	"\x41\x0f\xae\x5a\x38" + // stmxcsr 56(r10)
	"\x0f\xae\x15\x00\x00\x00\x00" + // ldmxcsr [rip+defaultMXCSR]
	"\x48\x89\xfb" + // mov rdi, rbx (linMem)
	"\x49\x8d\x7a\xf8" + // lea -8(r10), rdi
	"\x48\x89\x7b\xe8" // mov rdi, -24(rbx)

func tinygoIntThunk() string {
	const (
		saveA2 = "\x49\x89\xcb" // mov rcx, r11
		moveA0 = "\x48\x89\xf0" // mov rsi, rax
		moveA1 = "\x48\x89\xd1" // mov rdx, rcx
		moveA2 = "\x4c\x89\xda" // mov r11, rdx
	)
	switch buildfeatures.StandaloneFixedIntArgCount {
	case 0:
		return tinygoIntThunkPrologue + tinygoIntThunkEpilogue
	case 1:
		return tinygoIntThunkPrologue + moveA0 + tinygoIntThunkEpilogue
	case 2:
		return tinygoIntThunkPrologue + moveA0 + moveA1 + tinygoIntThunkEpilogue
	default:
		// The fourth argument already occupies R8.
		return tinygoIntThunkPrologue + saveA2 + moveA0 + moveA1 + moveA2 + tinygoIntThunkEpilogue
	}
}

const tinygoIntThunkEpilogue = "\x4c\x89\xd4" + // mov r10, rsp
	"\x31\xed" + // xor ebp, ebp
	"\x41\xff\xd1" + // call r9 (func-value context = code)
	"\x0f\xae\x54\x24\x38" + // ldmxcsr 56(rsp)
	"\x48\x8b\x6c\x24\x08" + // mov 8(rsp), rbp
	"\x48\x8b\x5c\x24\x10" + // mov 16(rsp), rbx
	"\x4c\x8b\x64\x24\x18" + // mov 24(rsp), r12
	"\x4c\x8b\x6c\x24\x20" + // mov 32(rsp), r13
	"\x4c\x8b\x74\x24\x28" + // mov 40(rsp), r14
	"\x4c\x8b\x7c\x24\x30" + // mov 48(rsp), r15
	"\x48\x8b\x24\x24" + // mov 0(rsp), rsp
	"\xc3" // ret

const tinygoIntThunkStackTopOff = 2

func preparedIntThunkFor(e *Engine) (uintptr, error) {
	if !tinygoPreparedIntForImage {
		return 0, errors.New("tinygo prepared integer entry is unavailable in this image")
	}
	// NewEngine initializes the executable mapping and keeps its base in
	// stackTop. Only an engine without a mapping needs the cold setup path.
	if e.preparedInt.mem == nil {
		if err := e.initNativeEntry(); err != nil {
			return 0, err
		}
	}
	return e.stackTop + tinygoPreparedIntEntryOffset, nil
}

func (e *Engine) EnterPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	entry, err := preparedIntThunkFor(e)
	if err != nil {
		return 0, err
	}
	fv := funcValue{context: code, fnptr: entry}
	call := *(*func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr)(unsafe.Pointer(&fv))
	return uint64(call(linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3))), nil
}

func (e *Engine) EnterPreparedIntBounded(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return e.EnterPreparedInt(code, linMemBase, a0, a1, a2, a3)
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
