//go:build (linux || darwin) && arm64 && tinygo

package runtime

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// TinyGo cannot assemble the Plan9 arm64 trampolines. Its indirect func calls
// use AAPCS64: X0-X3 carry the four explicit wrapper arguments and the func
// value context follows in X4. Generated thunks use that context as the native
// code pointer, switch to the engine's foreign stack, and preserve the AAPCS64
// callee-saved Go state around native execution.

type tinygoARM64FuncValue struct {
	context uintptr
	fnptr   uintptr
}

var (
	tinygoARM64PreparedIntEntryOffset = uintptr(len(tinygoARM64EntryCode(0)))
	tinygoMmapExec                    = mmapExec
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
	for reg, off := a64.X19, uint32(8); reg <= a64.LR; reg, off = reg+1, off+8 {
		tinygoARM64Store(a, reg, top, off)
	}
	for reg, off := a64.Reg(8), int32(104); reg <= a64.Reg(15); reg, off = reg+1, off+8 {
		a.FStoreDisp(top, off, reg, true)
	}
}

func tinygoARM64RestoreGoContext(a *a64.Asm) {
	for reg, off := a64.X19, uint32(8); reg <= a64.LR; reg, off = reg+1, off+8 {
		tinygoARM64Load(a, reg, a64.SP, off)
	}
	for reg, off := a64.Reg(8), int32(104); reg <= a64.Reg(15); reg, off = reg+1, off+8 {
		a.FLoadDisp(reg, a64.SP, off, true)
	}
	tinygoARM64Load(a, a64.X11, a64.SP, 0)
	a.AddImm64(a64.SP, a64.X11, 0)
	a.Ret()
}

func tinygoARM64EntryCode(foreignStackTop uintptr) []byte {
	var a a64.Asm
	a.Movz64(a64.X10, uint16(foreignStackTop), 0)
	a.Movk64(a64.X10, uint16(foreignStackTop>>16), 1)
	a.Movk64(a64.X10, uint16(foreignStackTop>>32), 2)
	a.Movk64(a64.X10, uint16(foreignStackTop>>48), 3)
	tinygoARM64SaveGoContext(&a, a64.X10)
	a.AddImm64(a64.SP, a64.X10, 0)
	a.MovImm64(a64.FP, 0)
	a.Stur64(a64.X10, a64.X1, -offTrapStackReentry)
	continuation := a.Adr(a64.X11)
	a.Stur64(a64.X11, a64.X1, -arm64TrapHandlerPtrOffset)
	a.Blr(a64.X4)
	epilogue := a.Len()
	if !a.PatchAdr(continuation, epilogue) {
		panic("wago: arm64 TinyGo trampoline continuation is out of range")
	}
	tinygoARM64RestoreGoContext(&a)
	return a.B
}

// initNativeEntry creates the wrapper and prepared-integer entry thunks in one
// Engine-owned executable mapping. Engine.Close releases the mapping with the
// 4 MiB foreign stack, so historical stack addresses retain no executable page.
func (e *Engine) initNativeEntry() error {
	if e.preparedInt.mem != nil {
		return nil
	}
	stackTop := e.stackTop
	entryCode := tinygoARM64EntryCode(stackTop)
	intCode := tinygoARM64IntEntryCode(stackTop)
	code := make([]byte, len(entryCode)+len(intCode))
	copy(code, entryCode)
	copy(code[len(entryCode):], intCode)
	mem, err := tinygoMmapExec(code)
	if err != nil {
		return fmt.Errorf("tinygo entry trampoline: %w", err)
	}
	e.preparedInt.mem = mem
	e.preparedInt.stackTop = stackTop
	e.stackTop = uintptr(unsafe.Pointer(&mem[0]))
	return nil
}

func enterNative(code, serArgs, linMem, trap, results, entry uintptr) {
	if entry == 0 {
		markTinyGoTrampolineFailure(trap)
		return
	}
	fv := tinygoARM64FuncValue{context: code, fnptr: entry}
	call := *(*func(a, b, c, d uintptr))(unsafe.Pointer(&fv))
	call(serArgs, linMem, trap, results)
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
