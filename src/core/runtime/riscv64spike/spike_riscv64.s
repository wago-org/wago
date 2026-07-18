//go:build linux && riscv64

#include "textflag.h"

// Save area at the top of the foreign stack:
//   0: Go SP; 8: GP; 16: TP; 24..96: S0..S9; 104: CTXT/S10;
//   112: g/S11; 120: RA.
#define SAVE_BYTES 128

// func enterNativeSpike(code, a0, a1, foreignStackTop uintptr) uintptr
TEXT ·enterNativeSpike(SB), NOSPLIT, $0-40
	MOV code+0(FP), T0
	MOV a0+8(FP), A0
	MOV a1+16(FP), A1
	MOV foreignStackTop+24(FP), T1

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
	JALR RA, T0

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

	MOV A0, ret+32(FP)
	RET

// func enterNativeMem(code, a0, a1, linMem, foreignStackTop uintptr) uintptr
TEXT ·enterNativeMem(SB), NOSPLIT, $0-48
	MOV code+0(FP), T0
	MOV a0+8(FP), A0
	MOV a1+16(FP), A1
	MOV linMem+24(FP), T3
	MOV foreignStackTop+32(FP), T1

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
	MOV T3, X25
	JALR RA, T0

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

	MOV A0, ret+40(FP)
	RET
