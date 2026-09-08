//go:build (linux || darwin || windows) && arm64 && !tinygo

#include "textflag.h"

// func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
TEXT ·enterNativeIntRaw(SB), NOSPLIT, $0-64
	MOVD code+0(FP), R9
	MOVD foreignStackTop+48(FP), R10
	SUB  $112, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	STP  (R19, R20), 8(R10)
	STP  (R21, R22), 24(R10)
	STP  (R23, R24), 40(R10)
	STP  (R25, R26), 56(R10)
	STP  (R27, g), 72(R10)
	STP  (R29, R30), 88(R10)

	MOVD linMem+8(FP), R26
	MOVD a0+16(FP), R0
	MOVD a1+24(FP), R1
	MOVD a2+32(FP), R2
	MOVD a3+40(FP), R3
	MOVD R10, RSP
	MOVD ZR, R22
	MOVD ZR, R29
	MOVD R10, -24(R26)
	BL   callNativeInt

afterNativeIntCall:
	LDP  8(RSP), (R19, R20)
	LDP  24(RSP), (R21, R22)
	LDP  40(RSP), (R23, R24)
	LDP  56(RSP), (R25, R26)
	LDP  72(RSP), (R27, g)
	LDP  88(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
	MOVD R0, ret+56(FP)
	RET

callNativeInt:
	MOVD R30, R11
	MOVD R11, -32(R26)
	BL   (R9)
	B    afterNativeIntCall
