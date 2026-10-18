//go:build linux && riscv64 && !tinygo

#include "textflag.h"
#include "go_asm.h"

// ABI0 adapter; Go ABIInternal arguments are not the guest a0/a1 convention.
// NOFRAME prevents a generated Go prologue from changing the saved RA/SP.
// The low bound, alignment and full declared guest descent are checked before
// any write to the foreign stack. g/gp/tp remain valid while native code runs.
TEXT ·enterRaw(SB),NOSPLIT|NOFRAME,$0-72
	MOV code+0(FP), T0
	MOV low+24(FP), T2
	MOV top+32(FP), T1
	MOV frame+40(FP), T3
	AND $15, T1, T4
	BNE T4, ZERO, bounds
	AND $15, T3, T4
	BNE T4, ZERO, bounds
	MOV $16, T4
	BLTU T3, T4, bounds
	BLTU T1, T2, bounds
	SUB T2, T1, T4
	MOV $const_bridgeSaveBytes, T5
	BLTU T4, T5, bounds
	SUB T5, T4, T4
	BLTU T4, T3, bounds

	ADD $-const_bridgeSaveBytes, T1, T1
	MOV X2, nativeSaveFrame_GoSP(T1)
	MOV RA, nativeSaveFrame_RA(T1)
	MOV GP, nativeSaveFrame_GP(T1)
	MOV TP, nativeSaveFrame_TP(T1)
	MOV X8, nativeSaveFrame_S0(T1)
	MOV X9, nativeSaveFrame_S1(T1)
	MOV X18, nativeSaveFrame_S2(T1)
	MOV X19, nativeSaveFrame_S3(T1)
	MOV X20, nativeSaveFrame_S4(T1)
	MOV X21, nativeSaveFrame_S5(T1)
	MOV X22, nativeSaveFrame_S6(T1)
	MOV X23, nativeSaveFrame_S7(T1)
	MOV X24, nativeSaveFrame_S8(T1)
	MOV X25, nativeSaveFrame_S9(T1)
	MOV CTXT, nativeSaveFrame_CTXT(T1)
	MOV g, nativeSaveFrame_G(T1)
	MOV a0+8(FP), A0
	MOV a1+16(FP), A1
	MOV trap+48(FP), A2
	MOV T2, A3
	MOV T1, X2
	JALR RA, T0

	MOV nativeSaveFrame_RA(X2), RA
	MOV nativeSaveFrame_GP(X2), GP
	MOV nativeSaveFrame_TP(X2), TP
	MOV nativeSaveFrame_S0(X2), X8
	MOV nativeSaveFrame_S1(X2), X9
	MOV nativeSaveFrame_S2(X2), X18
	MOV nativeSaveFrame_S3(X2), X19
	MOV nativeSaveFrame_S4(X2), X20
	MOV nativeSaveFrame_S5(X2), X21
	MOV nativeSaveFrame_S6(X2), X22
	MOV nativeSaveFrame_S7(X2), X23
	MOV nativeSaveFrame_S8(X2), X24
	MOV nativeSaveFrame_S9(X2), X25
	MOV nativeSaveFrame_CTXT(X2), CTXT
	MOV nativeSaveFrame_G(X2), g
	MOV nativeSaveFrame_GoSP(X2), T1
	MOV T1, X2
	MOV A0, result+56(FP)
	MOV ZERO, status+64(FP)
	RET
bounds:
	MOV ZERO, result+56(FP)
	MOV $1, T0
	MOV T0, status+64(FP)
	RET
