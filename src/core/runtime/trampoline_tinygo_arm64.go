//go:build (linux || darwin) && arm64 && tinygo

package runtime

import (
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

type tinygoARM64ThunkRef struct {
	top   uintptr
	entry uintptr
}

var (
	tinygoARM64LastThunk atomic.Pointer[tinygoARM64ThunkRef]
	tinygoARM64ThunkMu   sync.Mutex
	tinygoARM64Thunks    = map[uintptr]uintptr{}
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
}

func tinygoARM64RestoreGoContext(a *a64.Asm) {
	for reg, off := a64.X19, uint32(8); reg <= a64.LR; reg, off = reg+1, off+8 {
		tinygoARM64Load(a, reg, a64.SP, off)
	}
	tinygoARM64Load(a, a64.X11, a64.SP, 0)
	a.AddImm64(a64.SP, a64.X11, 0)
	a.Ret()
}

func tinygoARM64EntryCode(foreignStackTop uintptr) []byte {
	var a a64.Asm
	a.MovImm64(a64.X10, uint64(foreignStackTop))
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

func tinygoARM64ThunkFor(foreignStackTop uintptr) uintptr {
	if ref := tinygoARM64LastThunk.Load(); ref != nil && ref.top == foreignStackTop {
		return ref.entry
	}
	tinygoARM64ThunkMu.Lock()
	entry, ok := tinygoARM64Thunks[foreignStackTop]
	if !ok {
		mem, err := mmapExec(tinygoARM64EntryCode(foreignStackTop))
		if err != nil {
			tinygoARM64ThunkMu.Unlock()
			panic("wago: cannot map arm64 TinyGo trampoline: " + err.Error())
		}
		entry = uintptr(unsafe.Pointer(&mem[0]))
		tinygoARM64Thunks[foreignStackTop] = entry
	}
	tinygoARM64ThunkMu.Unlock()
	tinygoARM64LastThunk.Store(&tinygoARM64ThunkRef{top: foreignStackTop, entry: entry})
	return entry
}

func enterNative(code, serArgs, linMem, trap, results, foreignStackTop uintptr) {
	fv := tinygoARM64FuncValue{context: code, fnptr: tinygoARM64ThunkFor(foreignStackTop)}
	call := *(*func(a, b, c, d uintptr))(unsafe.Pointer(&fv))
	call(serArgs, linMem, trap, results)
}

var (
	tinygoARM64ResumeOnce  sync.Once
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
	tinygoARM64ResumeOnce.Do(func() {
		mem, err := mmapExec(tinygoARM64ResumeCode())
		if err != nil {
			panic("wago: cannot map arm64 TinyGo resume trampoline: " + err.Error())
		}
		tinygoARM64ResumeEntry = uintptr(unsafe.Pointer(&mem[0]))
	})
	return tinygoARM64ResumeEntry
}

func resumeNative(ctrl, foreignStackTop uintptr) {
	fv := tinygoARM64FuncValue{fnptr: tinygoARM64ResumePtr()}
	call := *(*func(a, b uintptr))(unsafe.Pointer(&fv))
	call(ctrl, foreignStackTop)
}
