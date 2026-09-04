//go:build amd64 && !tinygo

#include "textflag.h"

// func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
TEXT ·enterNativeIntRaw(SB), NOSPLIT, $0-64
	MOVQ code+0(FP), R11
	MOVQ foreignStackTop+48(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), SI
	MOVQ SI, -24(BX)
	MOVQ a0+16(FP), AX
	MOVQ a1+24(FP), CX
	MOVQ a2+32(FP), DX
	MOVQ a3+40(FP), R8

	MOVQ R10, SP
	XORL BP, BP
	CALL R11
	MOVQ AX, DI

	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
	MOVQ DI, ret+56(FP)
	RET
