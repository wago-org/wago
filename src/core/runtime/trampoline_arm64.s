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
	//
	// Only Go's callee-saved GP state is stashed here. Go's arm64 ABIInternal uses
	// F0-F15 for float args/results and F16-F31 as permanent scratch — none is
	// callee-saved — so the caller has already spilled any live V register and does
	// not expect V8-V15 preserved across this call. Native (AAPCS64) code clobbers
	// V8-V15 freely; we leave them clobbered on return, exactly like the amd64
	// trampoline leaves all XMM clobbered (System V has no callee-saved vector
	// register either). The [104,176) FP slots are therefore reserved but unused.
	SUB  $176, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	STP (R19, R20), 8(R10)
	STP (R21, R22), 24(R10)
	STP (R23, R24), 40(R10)
	STP (R25, R26), 56(R10)
	STP (R27, g), 72(R10)
	STP (R29, R30), 88(R10)

	MOVD R10, RSP
	MOVD ZR, R29

	MOVD R10, -24(R1)
	BL   callNative

afterNativeCall:
	LDP 8(RSP), (R19, R20)
	LDP 24(RSP), (R21, R22)
	LDP 40(RSP), (R23, R24)
	LDP 56(RSP), (R25, R26)
	LDP 72(RSP), (R27, g)
	LDP 88(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
	RET

callNative:
	MOVD R30, R11                  // afterNativeCall continuation PC
	MOVD R11, -32(R1)
	BL   (R9)
	B    afterNativeCall
