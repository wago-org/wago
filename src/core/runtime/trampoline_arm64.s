//go:build (linux || darwin || windows) && arm64 && !tinygo

#include "textflag.h"

// func enterNative(code, serArgs, linMem, trap, results, foreignStackTop uintptr)
//
// Enters arm64 WasmWrapper code on a dedicated off-heap foreign stack. The
// wrapper ABI is X0=serArgs, X1=linMem, X2=trap, X3=results. Native wasm code may
// freely use AAPCS64 callee-saved registers; X26 is the pinned linMem register.
// X28 remains Go's g register even while native code runs, so async signals and
// preemption see the expected Go context.
TEXT ·enterNativeRaw(SB), NOSPLIT, $0-48
	MOVD code+0(FP), R9
	MOVD serArgs+8(FP), R0
	MOVD linMem+16(FP), R1
	MOVD trap+24(FP), R2
	MOVD results+32(FP), R3
	MOVD foreignStackTop+40(FP), R10

	// Reserve a 176-byte save area at the top of the foreign stack. Native code
	// grows down from R10, so it does not touch this area on balanced returns.
	//
	// Go's ABI0 and ABIInternal both treat R19-R25 and R27 as scratch, as well
	// as every FP register. Preserve only SP, FP/LR and the closure-context
	// register; native code keeps g intact. Retain the existing 176-byte layout
	// because trap and resume continuations use its base as their landing SP.
	SUB  $176, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	MOVD R26, 64(R10)
	STP (R29, R30), 88(R10)

	MOVD R10, RSP
	MOVD ZR, R22                  // no active wasm exception handler at outer entry
	MOVD ZR, R29

	MOVD R10, -24(R1)
	BL   callNative

afterNativeCall:
	MOVD 64(RSP), R26
	LDP 88(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
	RET

callNative:
	MOVD R30, R11                  // afterNativeCall continuation PC
	MOVD R11, -32(R1)
	BL   (R9)
	B    afterNativeCall
