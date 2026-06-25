#include "textflag.h"

// func enterNative(code, serArgs, linMem, trap, results, foreignStackTop uintptr)
//
// Enters WARP-style native code on a dedicated, off-heap "foreign" stack.
// WARP-generated code uses the native stack directly (push / sub rsp / nested
// call+ret) with no Go prologue, stack maps, or stackguard — so it MUST NOT run
// on a goroutine stack (morestack would copy/relocate it; the GC would try to
// scan frames with no pointer maps). We switch RSP to a fixed mmap'd stack and
// switch back on return.
//
// The goroutine stays in _Grunning (no entersyscall). That is safe because:
//   - This is a NOSPLIT leaf with no Go safepoint, so the goroutine is not
//     migrated/preempted while inside it.
//   - Async-preempt (SIGURG) arriving while PC is in native code is declined
//     (findfunc fails for non-Go PC) and benignly resumed.
//   - getg() in signal handlers reads g from TLS, not R14, so signals stay
//     correct even though native code clobbers R14.
// The caller must keep every native run BOUNDED (a never-returning native loop
// would stall stop-the-world GC, which waits for this goroutine's safepoint).
//
// WasmWrapper System V mapping: serArgs->RDI, linMem->RSI, trap->RDX, results->RCX.
TEXT ·enterNative(SB), NOSPLIT, $0-48
	MOVQ code+0(FP), R11            // R11 = native entry (scratch; not an arg reg)
	MOVQ serArgs+8(FP), DI          // gpParams[0]
	MOVQ linMem+16(FP), SI          // gpParams[1] (becomes WasmABI linMem/RBX inside)
	MOVQ trap+24(FP), DX            // gpParams[2] -> TrapCode*
	MOVQ results+32(FP), CX         // gpParams[3] -> results*
	MOVQ foreignStackTop+40(FP), R10

	// Reserve a 64-byte save area at the top of the foreign stack and stash the
	// Go callee-saved registers + g + the goroutine SP there. Native code grows
	// the stack DOWN from R10, so it never touches this area above it.
	SUBQ $64, R10
	MOVQ SP,  0(R10)                // goroutine SP (points at our return address)
	MOVQ BP,  8(R10)                // goroutine frame pointer
	MOVQ BX, 16(R10)
	MOVQ R12, 24(R10)
	MOVQ R13, 32(R10)
	MOVQ R14, 40(R10)               // g
	MOVQ R15, 48(R10)

	// Switch to the foreign stack and zero RBP so any unwinder stops here.
	MOVQ R10, SP
	XORL BP, BP

	// Call the WARP WasmWrapper. It runs entirely on the foreign stack and must
	// balance it (RET leaves SP == R10).
	CALL R11

	// Restore Go context (SP currently == R10).
	MOVQ  8(SP), BP
	MOVQ 16(SP), BX
	MOVQ 24(SP), R12
	MOVQ 32(SP), R13
	MOVQ 40(SP), R14                // restore g
	MOVQ 48(SP), R15
	MOVQ  0(SP), SP                 // back on the goroutine stack
	PXOR X15, X15                   // re-zero the ABIInternal zero register
	RET
