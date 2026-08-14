//go:build arm64 && !tinygo && (linux || darwin || windows)

package runtime

func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
func enterNativeInt2(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) (uintptr, uintptr)
func enterNativeFP(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
func enterNativeFP2(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) (uintptr, uintptr)
func enterNativeMixed(code, linMem, g0, g1, f0, f1, foreignStackTop uintptr) (uintptr, uintptr)

func (e *Engine) EnterPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeInt(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)), nil
}

func (e *Engine) EnterPreparedInt2(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, uint64) {
	first, second := enterNativeInt2(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)
	return uint64(first), uint64(second)
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

func (e *Engine) EnterPreparedFP2(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, uint64) {
	first, second := enterNativeFP2(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)
	return uint64(first), uint64(second)
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
