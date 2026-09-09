//go:build arm64 && !tinygo && (linux || darwin || windows)

package runtime

func enterNativeIntRaw(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
func enterNativeIntLightRaw(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr

func (e *Engine) EnterPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeInt(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)), nil
}

// EnterPreparedIntLight uses the compiler's caller-clobber-only proof to avoid
// saving untouched callee-saved registers. The scheduler transition and the
// Go SP/X26/FP/LR preservation remain identical in contract to EnterPreparedInt.
func (e *Engine) EnterPreparedIntLight(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeIntLight(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)), nil
}

// EnterPreparedIntBounded is reserved for compiler-proven straight-line leaves:
// no loops, calls, memory/global access, EH, or custom instructions, with a
// tightly capped body. They cannot retain the P for an unbounded interval, so
// the full native transition need not enter syscall state.
func (e *Engine) EnterPreparedIntBounded(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeIntRaw(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)), nil
}

// EnterPreparedIntLightBounded combines the bounded-duration proof with the
// caller-clobber-only proof, avoiding both the scheduler transition and unused
// callee-saved register traffic.
func (e *Engine) EnterPreparedIntLightBounded(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeIntLightRaw(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)), nil
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
