//go:build amd64 && !tinygo && (linux || darwin || windows)

package runtime

// enterNativeInt enters a register-ABI integer leaf on the Engine foreign stack.
// RBX carries linMem, RAX/RCX/RDX/R8 carry up to four arguments, and RAX returns
// the optional scalar result.
func enterNativeIntRaw(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
func enterNativeIntPairRaw(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) (uintptr, uintptr)

//go:noescape
func enterNativeIntWideRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr, uintptr)

//go:noescape
func enterNativeIntOctRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr)

//go:noescape
func enterNativeFloatRaw(code, linMem uintptr, args *[4]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr)

//go:noescape
func enterNativeFloatOctRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr)

//go:noescape
func enterNativeMixedRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr)
func enterNativeIntPreboundContextRaw(call *PreparedIntCall, a0, a1, a2, a3 uintptr) uintptr
func enterNativeIntCallRaw(call *PreparedIntCall) uintptr

func (e *Engine) PrepareIntCall(call *PreparedIntCall, code, linMem uintptr) {
	call.code, call.linMem, call.stack = code, linMem, e.stackTop
}

// PrepareBoundedIntContext primes the trap-unwind stack address for a
// non-concurrent prepared handle. The prebound trampoline republishes it on
// every invocation because ordinary entries use a different foreign-stack frame
// and share the same basedata slot.
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

// EnterPreparedIntPairBounded enters a compiler-proven bounded integer function
// whose two results return in RAX/RDX. Callers inspect the trap cell afterward.
func (e *Engine) EnterPreparedIntPairBounded(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, uint64) {
	r0, r1 := enterNativeIntPairRaw(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop)
	return uint64(r0), uint64(r1)
}

// EnterPreparedIntWideBounded enters a compiler-proven bounded integer leaf
// with up to seven register arguments and five register results.
func (e *Engine) EnterPreparedIntWideBounded(code, linMemBase uintptr, args *[8]uint64) (uint64, uint64, uint64, uint64, uint64) {
	r0, r1, r2, r3, r4 := enterNativeIntWideRaw(code, linMemBase, args, e.stackTop)
	return uint64(r0), uint64(r1), uint64(r2), uint64(r3), uint64(r4)
}

// EnterPreparedIntOctBounded enters a compiler-proven bounded integer leaf
// returning six or seven values through the register bank.
func (e *Engine) EnterPreparedIntOctBounded(code, linMemBase uintptr, args *[8]uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64) {
	r0, r1, r2, r3, r4, r5, r6, r7 := enterNativeIntOctRaw(code, linMemBase, args, e.stackTop)
	return uint64(r0), uint64(r1), uint64(r2), uint64(r3), uint64(r4), uint64(r5), uint64(r6), uint64(r7)
}

// EnterPreparedFloatBounded enters a compiler-proven bounded FP leaf with up
// to four FP register arguments and four FP register results.
func (e *Engine) EnterPreparedFloatBounded(code, linMemBase uintptr, args *[4]uint64) (uint64, uint64, uint64, uint64) {
	r0, r1, r2, r3 := enterNativeFloatRaw(code, linMemBase, args, e.stackTop)
	return uint64(r0), uint64(r1), uint64(r2), uint64(r3)
}

// EnterPreparedFloatOctBounded enters a bounded FP leaf using all eight FP registers.
func (e *Engine) EnterPreparedFloatOctBounded(code, linMemBase uintptr, args *[8]uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64) {
	r0, r1, r2, r3, r4, r5, r6, r7 := enterNativeFloatOctRaw(code, linMemBase, args, e.stackTop)
	return uint64(r0), uint64(r1), uint64(r2), uint64(r3), uint64(r4), uint64(r5), uint64(r6), uint64(r7)
}

// EnterPreparedMixedBounded stages up to four GP and four FP arguments in
// independent register banks. The caller selects the result bank by signature.
func (e *Engine) EnterPreparedMixedBounded(code, linMemBase uintptr, args *[8]uint64) (uint64, uint64, uint64, uint64) {
	gp0, gp1, fp0, fp1 := enterNativeMixedRaw(code, linMemBase, args, e.stackTop)
	return uint64(gp0), uint64(gp1), uint64(fp0), uint64(fp1)
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
