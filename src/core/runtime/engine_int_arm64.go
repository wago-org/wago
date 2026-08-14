//go:build arm64 && !tinygo && (linux || darwin || windows)

package runtime

func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
func enterNativeFP(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr

func (e *Engine) EnterPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeInt(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)), nil
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

func ConsumePreparedIntTrap(trap []byte) error {
	tc := PreparedIntTrapCode(trap)
	if tc == TrapNone {
		return nil
	}
	storeTrap(trap, 0)
	return trapErrorFromBuffer(tc, trap)
}
