//go:build !tinygo

#include "textflag.h"

// func testEnterNativeIntWithMXCSR(call *testMXCSRRawCall)
//
// Installs a deliberately non-Go control state only across the raw assembly
// boundary. The original Go MXCSR is restored before this helper returns.
TEXT ·testEnterNativeIntWithMXCSR(SB), NOSPLIT, $72-8
	MOVQ call+0(FP), BX
	STMXCSR 64(SP)

	MOVQ  0(BX), AX
	MOVQ AX,  0(SP)
	MOVQ  8(BX), AX
	MOVQ AX,  8(SP)
	MOVQ $0, 16(SP)
	MOVQ $0, 24(SP)
	MOVQ $0, 32(SP)
	MOVQ $0, 40(SP)
	MOVQ 16(BX), AX
	MOVQ AX, 48(SP)

	LDMXCSR 24(BX)
	CALL ·enterNativeIntRaw(SB)
	MOVQ 56(SP), AX
	MOVQ AX, 32(BX)
	STMXCSR 28(BX)
	LDMXCSR 64(SP)
	RET
