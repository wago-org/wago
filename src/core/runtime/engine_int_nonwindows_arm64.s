//go:build (linux || darwin) && arm64 && !tinygo

#include "textflag.h"

// func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
TEXT ·enterNativeInt(SB), NOSPLIT, $0-64
	MOVD code+0(FP), R9
	MOVD foreignStackTop+48(FP), R10
	// RailMach's internal entry preserves every allocated AAPCS64 callee-save.
	// Save only the registers the transition itself overwrites: X22 is the guest
	// exception link, X26 is linMem, and X29/LR are cleared/used for the call.
	// X28 remains Go's g register throughout native execution.
	SUB  $48, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	STP  (R22, R26), 8(R10)
	STP  (R29, R30), 24(R10)

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
	LDP  8(RSP), (R22, R26)
	LDP  24(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
	MOVD R0, ret+56(FP)
	RET

callNativeInt:
	MOVD R30, R11
	MOVD R11, -32(R26)
	// R30 already names afterNativeIntCall. Tail-enter the guest so its RET
	// returns directly to the restore path without an intermediate branch.
	B    (R9)
