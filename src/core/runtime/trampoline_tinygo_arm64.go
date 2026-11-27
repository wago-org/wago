// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64 && tinygo

package runtime

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/internal/buildfeatures"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

const tinygoPreparedIntForImage = buildfeatures.StandaloneDirectIntFixed
const tinygoPreparedLightIntForImage = tinygoPreparedIntForImage && buildfeatures.StandaloneDirectIntBoundedLight

// TinyGo cannot assemble the Plan9 arm64 trampolines. Its indirect func calls
// use AAPCS64: X0-X3 carry the wrapper arguments, X4 carries the foreign stack
// address, and X5 carries the function value's native-code pointer.

type tinygoARM64FuncValue struct {
	context uintptr
	fnptr   uintptr
}

type tinygoARM64EntryPoints struct {
	wrapper  uintptr
	lightInt uintptr
	fullInt  uintptr
}

var (
	tinygoARM64Entries atomic.Pointer[tinygoARM64EntryPoints]
	tinygoARM64EntryMu sync.Mutex
	tinygoMmapExec     = mmapExec
)

func tinygoARM64Store(a *a64.Asm, src, base a64.Reg, off uint32) {
	if !a.Store64(src, base, off) {
		panic("wago: arm64 TinyGo trampoline store is not encodable")
	}
}

func tinygoARM64Load(a *a64.Asm, dst, base a64.Reg, off uint32) {
	if !a.Load64(dst, base, off) {
		panic("wago: arm64 TinyGo trampoline load is not encodable")
	}
}

func tinygoARM64SaveGoContext(a *a64.Asm, top a64.Reg) {
	a.SubImm64(top, top, 176)
	a.AddImm64(a64.X11, a64.SP, 0)
	tinygoARM64Store(a, a64.X11, top, 0)
	for reg, off := a64.X19, int32(8); reg < a64.LR; reg, off = reg+2, off+16 {
		a.StpOffset(reg, reg+1, top, off)
	}
	for reg, off := a64.Reg(8), int32(104); reg < 15; reg, off = reg+2, off+16 {
		a.FStpOffset64(reg, reg+1, top, off)
	}
}

func tinygoARM64RestoreGoContext(a *a64.Asm) {
	for reg, off := a64.X19, int32(8); reg < a64.LR; reg, off = reg+2, off+16 {
		a.LdpOffset(reg, reg+1, a64.SP, off)
	}
	for reg, off := a64.Reg(8), int32(104); reg < 15; reg, off = reg+2, off+16 {
		a.FLdpOffset64(reg, reg+1, a64.SP, off)
	}
	tinygoARM64Load(a, a64.X11, a64.SP, 0)
	a.AddImm64(a64.SP, a64.X11, 0)
	a.Ret()
}

func tinygoARM64EntryCode() []byte {
	var a a64.Asm
	a.MovReg64(a64.X10, a64.X4)
	tinygoARM64SaveGoContext(&a, a64.X10)
	a.AddImm64(a64.SP, a64.X10, 0)
	a.MovImm64(a64.FP, 0)
	a.Stur64(a64.X10, a64.X1, -offTrapStackReentry)
	continuation := a.Adr(a64.X11)
	a.Stur64(a64.X11, a64.X1, -arm64TrapHandlerPtrOffset)
	a.Blr(a64.X5)
	epilogue := a.Len()
	if !a.PatchAdr(continuation, epilogue) {
		panic("wago: arm64 TinyGo trampoline continuation is out of range")
	}
	tinygoARM64RestoreGoContext(&a)
	return a.B
}

// The thunk code has no Engine-specific addresses. Every Engine keeps the
// physical top of its own foreign stack and passes it on entry.
func (*Engine) initNativeEntry() error {
	if tinygoARM64Entries.Load() != nil {
		return nil
	}
	return initTinyGoARM64Entries()
}

//go:noinline
func initTinyGoARM64Entries() error {
	tinygoARM64EntryMu.Lock()
	defer tinygoARM64EntryMu.Unlock()
	if tinygoARM64Entries.Load() != nil {
		return nil
	}
	wrapper := tinygoARM64EntryCode()
	var lightInt, fullInt []byte
	if tinygoPreparedLightIntForImage {
		lightInt = tinygoARM64IntEntryCode(true)
	}
	if tinygoPreparedIntForImage {
		fullInt = tinygoARM64IntEntryCode(false)
	}
	code := make([]byte, 0, len(wrapper)+len(lightInt)+len(fullInt))
	code = append(code, wrapper...)
	code = append(code, lightInt...)
	code = append(code, fullInt...)
	mem, err := tinygoMmapExec(code)
	if err != nil {
		return fmt.Errorf("tinygo entry trampoline: %w", err)
	}
	// This one executable image stays mapped for the process lifetime.
	base := uintptr(unsafe.Pointer(&mem[0]))
	entries := &tinygoARM64EntryPoints{wrapper: base}
	if tinygoPreparedLightIntForImage {
		entries.lightInt = base + uintptr(len(wrapper))
	}
	if tinygoPreparedIntForImage {
		entries.fullInt = base + uintptr(len(wrapper)+len(lightInt))
	}
	tinygoARM64Entries.Store(entries)
	return nil
}

func enterNative(code, serArgs, linMem, trap, results, stackTop uintptr) {
	entries := tinygoARM64Entries.Load()
	if entries == nil || stackTop == 0 {
		markTinyGoTrampolineFailure(trap)
		return
	}
	fv := tinygoARM64FuncValue{context: code, fnptr: entries.wrapper}
	call := *(*func(a, b, c, d, e uintptr))(unsafe.Pointer(&fv))
	call(serArgs, linMem, trap, results, stackTop)
}

var (
	tinygoARM64ResumeMu    sync.Mutex
	tinygoARM64ResumeEntry uintptr
)

func tinygoARM64ResumeCode() []byte {
	var a a64.Asm
	a.MovReg64(a64.X9, a64.X0)
	a.MovReg64(a64.X10, a64.X1)
	tinygoARM64SaveGoContext(&a, a64.X10)
	tinygoARM64Load(&a, a64.X26, a64.X9, hcSavedLinMem)
	a.Stur64(a64.X10, a64.X26, -offTrapStackReentry)
	continuation := a.Adr(a64.X11)
	a.Stur64(a64.X11, a64.X26, -arm64TrapHandlerPtrOffset)

	for reg, off := a64.X19, uint32(hcSavedX19); reg <= a64.X27; reg, off = reg+1, off+8 {
		tinygoARM64Load(&a, reg, a64.X9, off)
	}
	tinygoARM64Load(&a, a64.FP, a64.X9, hcSavedFP)
	tinygoARM64Load(&a, a64.LR, a64.X9, hcSavedLR)
	for reg, off := a64.Reg(8), int32(hcSavedV8); reg <= a64.Reg(15); reg, off = reg+1, off+8 {
		a.FLoadDisp(reg, a64.X9, off, true)
	}
	tinygoARM64Load(&a, a64.X11, a64.X9, hcSavedSP)
	a.AddImm64(a64.SP, a64.X11, 0)
	a.Br(a64.LR)

	epilogue := a.Len()
	if !a.PatchAdr(continuation, epilogue) {
		panic("wago: arm64 TinyGo resume continuation is out of range")
	}
	tinygoARM64RestoreGoContext(&a)
	return a.B
}

func tinygoARM64ResumePtr() uintptr {
	if entry := atomic.LoadUintptr(&tinygoARM64ResumeEntry); entry != 0 {
		return entry
	}
	tinygoARM64ResumeMu.Lock()
	defer tinygoARM64ResumeMu.Unlock()
	if entry := atomic.LoadUintptr(&tinygoARM64ResumeEntry); entry != 0 {
		return entry
	}
	mem, err := tinygoMmapExec(tinygoARM64ResumeCode())
	if err != nil {
		return 0
	}
	entry := uintptr(unsafe.Pointer(&mem[0]))
	atomic.StoreUintptr(&tinygoARM64ResumeEntry, entry)
	return entry
}

func resumeNative(ctrl, foreignStackTop uintptr) {
	entry := tinygoARM64ResumePtr()
	if entry == 0 {
		markTinyGoResumeFailure(ctrl)
		return
	}
	fv := tinygoARM64FuncValue{fnptr: entry}
	call := *(*func(a, b uintptr))(unsafe.Pointer(&fv))
	call(ctrl, foreignStackTop)
}
