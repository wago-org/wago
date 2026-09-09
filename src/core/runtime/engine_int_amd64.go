//go:build amd64 && !tinygo && (linux || darwin || windows)

package runtime

// enterNativeInt enters a register-ABI integer leaf on the Engine foreign stack.
// RBX carries linMem, RAX/RCX/RDX/R8 carry up to four arguments, and RAX returns
// the optional scalar result.
func enterNativeIntRaw(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
func enterNativeIntPreboundContextRaw(call *PreparedIntCall, a0, a1, a2, a3 uintptr) uintptr
func enterNativeIntCallRaw(call *PreparedIntCall) uintptr

func (e *Engine) PrepareIntCall(call *PreparedIntCall, code, linMem uintptr) {
	call.code, call.linMem, call.stack = code, linMem, e.stackTop
}

// PrepareBoundedIntContext binds the immutable trap-unwind stack address for a
// non-concurrent prepared handle. Engine stackTop and the instance's basedata
// mapping are stable until their owners close; every ordinary entry would store
// this same value immediately before its native CALL.
func (e *Engine) PrepareBoundedIntContext(linMem uintptr) {
	storeOffHeapU64(linMem-offTrapStackReentry, uint64(e.stackTop-40))
}

func (*Engine) EnterPreparedIntCallBounded(call *PreparedIntCall, a0, a1, a2, a3 uint64) uint64 {
	call.a0, call.a1, call.a2, call.a3 = uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3)
	return uint64(enterNativeIntCallRaw(call))
}

// EnterPreparedInt performs only the native transition. Callers must inspect
// PreparedIntTrapCode immediately afterward and consume any non-zero trap.
func (e *Engine) EnterPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeInt(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)), nil
}

// EnterPreparedIntBounded is reserved for compiler-proven bounded leaves: no
// loops, calls, bulk operations, table mutation, linear-memory access, EH, or
// custom instructions, with a tightly capped body. They cannot retain the P for
// an unbounded interval, so the full native transition need not enter syscall
// state.
func (e *Engine) EnterPreparedIntBounded(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return uint64(enterNativeIntRaw(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)), nil
}

// EnterPreparedIntPreboundContextBounded reads immutable entry state from call
// while keeping per-invocation arguments out of the prepared block.
func (*Engine) EnterPreparedIntPreboundContextBounded(call *PreparedIntCall, a0, a1, a2, a3 uint64) uint64 {
	return uint64(enterNativeIntPreboundContextRaw(call, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3)))
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
