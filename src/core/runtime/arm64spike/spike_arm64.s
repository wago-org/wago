//go:build (linux || darwin) && arm64

#include "textflag.h"

// func enterNativeSpike(code, a0, a1, foreignStackTop uintptr) uintptr
//
// AArch64 twin of the amd64 enterNative trampoline. Runs native (railshot-arm64)
// code on a dedicated off-heap stack: such code uses the stack directly with no
// Go prologue/stackmaps, so it MUST NOT run on a goroutine stack. We switch RSP
// to the foreign stack and switch back on return.
TEXT ·enterNativeSpike(SB), NOSPLIT, $0-40
	MOVD code+0(FP), R9
	MOVD a0+8(FP), R0                // AAPCS64 gpParams[0]
	MOVD a1+16(FP), R1              // AAPCS64 gpParams[1]
	MOVD foreignStackTop+24(FP), R10

	// Reserve a 112-byte save area at the top of the foreign stack (16-aligned)
	// and stash the goroutine SP + Go callee-saved registers + g + fp + lr.
	// Native code grows the stack DOWN from R10, so it never touches this area.
	SUB  $112, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)               // goroutine SP
	MOVD R19, 8(R10)
	MOVD R20, 16(R10)
	MOVD R21, 24(R10)
	MOVD R22, 32(R10)
	MOVD R23, 40(R10)
	MOVD R24, 48(R10)
	MOVD R25, 56(R10)
	MOVD R26, 64(R10)
	MOVD g, 72(R10)                // g (R28)
	MOVD R29, 80(R10)              // frame pointer
	MOVD R30, 88(R10)              // link register (Go return address)

	MOVD R10, RSP                  // switch to the foreign stack
	MOVD ZR, R29                   // zero fp so an unwinder stops here

	BL   (R9)                      // call native code; result in R0 (RSP back to R10 on return)

	// Restore the Go context (RSP == R10 after the balanced call).
	MOVD 8(RSP), R19
	MOVD 16(RSP), R20
	MOVD 24(RSP), R21
	MOVD 32(RSP), R22
	MOVD 40(RSP), R23
	MOVD 48(RSP), R24
	MOVD 56(RSP), R25
	MOVD 64(RSP), R26
	MOVD 72(RSP), g                // restore g (R28)
	MOVD 80(RSP), R29              // restore fp
	MOVD 88(RSP), R30              // restore lr
	MOVD 0(RSP), R11
	MOVD R11, RSP                  // back on the goroutine stack

	MOVD R0, ret+32(FP)
	RET

// func enterNativeMem(code, a0, a1, linMem, foreignStackTop uintptr) uintptr
//
// Like enterNativeSpike, but also sets X26 (= linMem base) before the call. The
// wago ABI keeps the linear-memory base pinned in X26 for the whole function, and
// the register-ABI internal entry assumes it is already established. X28 remains
// Go's g register while native code runs.
TEXT ·enterNativeMem(SB), NOSPLIT, $0-48
	MOVD code+0(FP), R9
	MOVD a0+8(FP), R0
	MOVD a1+16(FP), R1
	MOVD linMem+24(FP), R12
	MOVD foreignStackTop+32(FP), R10

	SUB  $112, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	MOVD R19, 8(R10)
	MOVD R20, 16(R10)
	MOVD R21, 24(R10)
	MOVD R22, 32(R10)
	MOVD R23, 40(R10)
	MOVD R24, 48(R10)
	MOVD R25, 56(R10)
	MOVD R26, 64(R10)
	MOVD g, 72(R10)
	MOVD R29, 80(R10)
	MOVD R30, 88(R10)

	MOVD R10, RSP
	MOVD ZR, R29
	MOVD R12, R26                 // X26 = linMem base

	BL   (R9)

	MOVD 8(RSP), R19
	MOVD 16(RSP), R20
	MOVD 24(RSP), R21
	MOVD 32(RSP), R22
	MOVD 40(RSP), R23
	MOVD 48(RSP), R24
	MOVD 56(RSP), R25
	MOVD 64(RSP), R26
	MOVD 72(RSP), g
	MOVD 80(RSP), R29
	MOVD 88(RSP), R30
	MOVD 0(RSP), R11
	MOVD R11, RSP

	MOVD R0, ret+40(FP)
	RET
