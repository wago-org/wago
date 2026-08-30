//go:build (linux || darwin) && arm64 && tinygo

package runtime

import (
	"unsafe"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// TinyGo passes the five explicit uintptr arguments in X0-X4 and the func-value
// context in X5. This thunk shifts them into Wago's direct integer ABI, saves the
// complete AAPCS64 Go context, and enters native code on the foreign stack.
func tinygoARM64IntEntryCode(foreignStackTop uintptr) []byte {
	var a a64.Asm
	a.MovReg64(a64.X9, a64.X0) // preserve linMem while shifting arguments
	a.MovReg64(a64.X0, a64.X1)
	a.MovReg64(a64.X1, a64.X2)
	a.MovReg64(a64.X2, a64.X3)
	a.MovReg64(a64.X3, a64.X4)
	a.MovImm64(a64.X10, uint64(foreignStackTop))
	tinygoARM64SaveGoContext(&a, a64.X10)
	a.MovReg64(a64.X26, a64.X9)
	a.AddImm64(a64.SP, a64.X10, 0)
	a.MovReg64(a64.X22, a64.ZR)
	a.MovReg64(a64.FP, a64.ZR)
	a.Stur64(a64.X10, a64.X26, -offTrapStackReentry)
	continuation := a.Adr(a64.X11)
	a.Stur64(a64.X11, a64.X26, -arm64TrapHandlerPtrOffset)
	a.Blr(a64.X5)
	epilogue := a.Len()
	if !a.PatchAdr(continuation, epilogue) {
		panic("wago: arm64 TinyGo integer trampoline continuation is out of range")
	}
	tinygoARM64RestoreGoContext(&a)
	return a.B
}

func preparedIntThunkFor(e *Engine) (uintptr, error) {
	if err := e.initNativeEntry(); err != nil {
		return 0, err
	}
	return uintptr(unsafe.Pointer(&e.preparedInt.mem[0])) + tinygoARM64PreparedIntEntryOffset, nil
}

func (e *Engine) EnterPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	entry, err := preparedIntThunkFor(e)
	if err != nil {
		return 0, err
	}
	fv := tinygoARM64FuncValue{context: code, fnptr: entry}
	call := *(*func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr)(unsafe.Pointer(&fv))
	return uint64(call(linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3))), nil
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
