//go:build amd64 && !tinygo && (linux || darwin || windows)

package runtime

// enterNativeInt enters a register-ABI integer leaf on the Engine foreign stack.
// RBX carries linMem, RAX/RCX/RDX/R8 carry up to four arguments, and RAX returns
// the optional scalar result.
func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
func enterNativeFP(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
func enterNativeMixed(code, linMem, g0, g1, f0, f1, foreignStackTop uintptr) (uintptr, uintptr)

// EnterPreparedInt performs only the native transition. Callers must inspect
// PreparedIntTrapCode immediately afterward and consume any non-zero trap.
func (e *Engine) EnterPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeInt(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)), nil
}

func (e *Engine) EnterPreparedMixed(code, linMemBase uintptr, g0, g1, f0, f1 uint64) (uint64, uint64) {
	gp, fp := enterNativeMixed(code, linMemBase, uintptr(g0), uintptr(g1), uintptr(f0), uintptr(f1), e.stackTop)
	return uint64(gp), uint64(fp)
}

// EnterPreparedFP enters an FP-only register-ABI function. Arguments and the
// optional result retain their raw IEEE-754 bits at the Go boundary.
func (e *Engine) EnterPreparedFP(code, linMemBase uintptr, a0, a1, a2, a3 uint64) uint64 {
	return uint64(enterNativeFP(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop))
}

func PreparedIntTrapCode(trap []byte) TrapCode {
	if len(trap) < 4 {
		return TrapNone
	}
	return TrapCode(loadTrap(trap))
}

// ConsumePreparedIntTrap is the cold half of prepared integer trap handling.
func ConsumePreparedIntTrap(trap []byte) error {
	tc := PreparedIntTrapCode(trap)
	if tc == TrapNone {
		return nil
	}
	storeTrap(trap, 0)
	return trapErrorFromBuffer(tc, trap)
}
