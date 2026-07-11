//go:build amd64 && !tinygo

#include "textflag.h"

// func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)
TEXT ·cpuid(SB), NOSPLIT, $0-24
	MOVL eaxArg+0(FP), AX
	MOVL ecxArg+4(FP), CX
	CPUID
	MOVL AX, eax+8(FP)
	MOVL BX, ebx+12(FP)
	MOVL CX, ecx+16(FP)
	MOVL DX, edx+20(FP)
	RET

// func xgetbvLow() uint32
// Returns XCR0[31:0] (the OS-enabled extended-state mask), reading with ECX=0.
// XGETBV has no Go mnemonic on all toolchains, so emit its bytes directly.
TEXT ·xgetbvLow(SB), NOSPLIT, $0-4
	MOVL $0, CX
	BYTE $0x0F
	BYTE $0x01
	BYTE $0xD0 // XGETBV -> EDX:EAX
	MOVL AX, ret+0(FP)
	RET
