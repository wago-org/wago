//go:build linux && riscv64 && !tinygo

#include "textflag.h"

// Foreign-stack save area. The first 128 bytes preserve every Go/psABI
// callee-saved register plus GP, TP, CTXT, g, RA, and the original Go SP. The
// remaining bytes retain layout parity with the host-call control protocol.
#define SAVE_BYTES 176

// func enterNative(code, serArgs, linMem, trap, results, foreignStackTop uintptr)
TEXT ·enterNative(SB), NOSPLIT, $0-48
	MOV code+0(FP), T0
	MOV serArgs+8(FP), A0
	MOV linMem+16(FP), A1
	MOV trap+24(FP), A2
	MOV results+32(FP), A3
	MOV foreignStackTop+40(FP), T1

	SUB $SAVE_BYTES, T1, T1
	MOV SP, T2
	MOV T2, 0(T1)
	MOV GP, 8(T1)
	MOV TP, 16(T1)
	MOV X8, 24(T1)
	MOV X9, 32(T1)
	MOV X18, 40(T1)
	MOV X19, 48(T1)
	MOV X20, 56(T1)
	MOV X21, 64(T1)
	MOV X22, 72(T1)
	MOV X23, 80(T1)
	MOV X24, 88(T1)
	MOV X25, 96(T1)
	MOV CTXT, 104(T1)
	MOV g, 112(T1)
	MOV RA, 120(T1)

	MOV T1, SP
	MOV T1, -24(A1) // trap/host re-entry restores this foreign-stack base
	JAL RA, callNative

afterNativeCall:
	MOV 8(SP), GP
	MOV 16(SP), TP
	MOV 24(SP), X8
	MOV 32(SP), X9
	MOV 40(SP), X18
	MOV 48(SP), X19
	MOV 56(SP), X20
	MOV 64(SP), X21
	MOV 72(SP), X22
	MOV 80(SP), X23
	MOV 88(SP), X24
	MOV 96(SP), X25
	MOV 104(SP), CTXT
	MOV 112(SP), g
	MOV 120(SP), RA
	MOV 0(SP), T2
	MOV T2, SP
	RET

callNative:
	MOV RA, T2
	MOV T2, -32(A1) // continuation used by cold trap/host unwind
	JALR RA, T0
	JMP afterNativeCall

// func resumeNative(ctrl, foreignStackTop uintptr)
TEXT ·resumeNative(SB), NOSPLIT, $0-16
	MOV ctrl+0(FP), T0
	MOV foreignStackTop+8(FP), T1

	SUB $SAVE_BYTES, T1, T1
	MOV SP, T2
	MOV T2, 0(T1)
	MOV GP, 8(T1)
	MOV TP, 16(T1)
	MOV X8, 24(T1)
	MOV X9, 32(T1)
	MOV X18, 40(T1)
	MOV X19, 48(T1)
	MOV X20, 56(T1)
	MOV X21, 64(T1)
	MOV X22, 72(T1)
	MOV X23, 80(T1)
	MOV X24, 88(T1)
	MOV X25, 96(T1)
	MOV CTXT, 104(T1)
	MOV g, 112(T1)
	MOV RA, 120(T1)

	MOV 80(T0), X25 // parked S9/linMem
	MOV T1, -24(X25)
	MOV T1, SP
	JAL RA, resumeWasm

afterResume:
	MOV 8(SP), GP
	MOV 16(SP), TP
	MOV 24(SP), X8
	MOV 32(SP), X9
	MOV 40(SP), X18
	MOV 48(SP), X19
	MOV 56(SP), X20
	MOV 64(SP), X21
	MOV 72(SP), X22
	MOV 80(SP), X23
	MOV 88(SP), X24
	MOV 96(SP), X25
	MOV 104(SP), CTXT
	MOV 112(SP), g
	MOV 120(SP), RA
	MOV 0(SP), T2
	MOV T2, SP
	RET

resumeWasm:
	MOV RA, T2
	MOV T2, -32(X25)
	MOV 8(T0), X8
	MOV 16(T0), X9
	MOV 24(T0), X18
	MOV 32(T0), X19
	MOV 40(T0), X20
	MOV 48(T0), X21
	MOV 56(T0), X22
	MOV 64(T0), X23
	MOV 72(T0), X24
	MOV 80(T0), X25
	MOV 96(T0), RA
	MOV 0(T0), T2
	MOV T2, SP
	JALR ZERO, RA
