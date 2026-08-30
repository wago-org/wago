//go:build arm64 && !tinygo && (linux || darwin || windows)

package runtime

func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
func enterNativeLeafInt(code, a0, a1, a2, a3 uintptr) uintptr

// EnterPreparedLeafInt enters a compiler-proven call-free, trap-free integer
// leaf directly on the Go stack. Such a leaf neither observes guest context nor
// requires a foreign stack, so the full guest transition is unnecessary.
func (e *Engine) EnterPreparedLeafInt(code, _ uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeLeafInt(code, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3))), nil
}

func (e *Engine) EnterPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeInt(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)), nil
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
