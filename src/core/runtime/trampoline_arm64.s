//go:build (linux || darwin) && arm64 && !tinygo

#include "textflag.h"

// func enterNative(code, serArgs, linMem, trap, results, foreignStackTop uintptr)
//
// Enters arm64 WasmWrapper code on a dedicated off-heap foreign stack. The
// wrapper ABI is X0=serArgs, X1=linMem, X2=trap, X3=results. Native wasm code may
// freely use AAPCS64 callee-saved registers; X26 is the pinned linMem register.
// X28 remains Go's g register even while native code runs, so async signals and
// preemption see the expected Go context.
TEXT ·enterNative(SB), NOSPLIT, $0-48
	MOVD code+0(FP), R9
	MOVD serArgs+8(FP), R0
	MOVD linMem+16(FP), R1
	MOVD trap+24(FP), R2
	MOVD results+32(FP), R3
	MOVD foreignStackTop+40(FP), R10

	// Reserve a 176-byte save area at the top of the foreign stack. Native code
	// grows down from R10, so it does not touch this area on balanced returns.
	SUB  $176, R10, R10
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
	MOVD R27, 72(R10)
	MOVD g, 80(R10)
	MOVD R29, 88(R10)
	MOVD R30, 96(R10)
	FMOVD F8, 104(R10)
	FMOVD F9, 112(R10)
	FMOVD F10, 120(R10)
	FMOVD F11, 128(R10)
	FMOVD F12, 136(R10)
	FMOVD F13, 144(R10)
	FMOVD F14, 152(R10)
	FMOVD F15, 160(R10)

	MOVD R10, RSP
	MOVD ZR, R29

	MOVD R10, -24(R1)
	BL   callNative

afterNativeCall:
	FMOVD 104(RSP), F8
	FMOVD 112(RSP), F9
	FMOVD 120(RSP), F10
	FMOVD 128(RSP), F11
	FMOVD 136(RSP), F12
	FMOVD 144(RSP), F13
	FMOVD 152(RSP), F14
	FMOVD 160(RSP), F15
	MOVD 8(RSP), R19
	MOVD 16(RSP), R20
	MOVD 24(RSP), R21
	MOVD 32(RSP), R22
	MOVD 40(RSP), R23
	MOVD 48(RSP), R24
	MOVD 56(RSP), R25
	MOVD 64(RSP), R26
	MOVD 72(RSP), R27
	MOVD 80(RSP), g
	MOVD 88(RSP), R29
	MOVD 96(RSP), R30
	MOVD 0(RSP), R11
	MOVD R11, RSP
	RET

callNative:
	MOVD R30, R11                  // afterNativeCall continuation PC
	MOVD R11, -32(R1)
	BL   (R9)
	B    afterNativeCall
