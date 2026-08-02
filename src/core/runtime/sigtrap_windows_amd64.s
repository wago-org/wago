//go:build windows && amd64 && wago_guardpage

#include "textflag.h"

// Windows VEH ABI: CX=*EXCEPTION_POINTERS, return AX is
// EXCEPTION_CONTINUE_EXECUTION (-1) or EXCEPTION_CONTINUE_SEARCH (0).
TEXT ·guardExceptionHandler(SB), NOSPLIT|NOFRAME, $0-0
	PUSHQ	BX
	PUSHQ	R12
	PUSHQ	R13
	PUSHQ	R14
	PUSHQ	R15
	MOVQ	CX, R12
	MOVQ	0(CX), R13              // EXCEPTION_RECORD
	MOVL	0(R13), AX
	CMPL	AX, $0xc0000005         // EXCEPTION_ACCESS_VIOLATION
	JNE	search
	CMPL	24(R13), $2
	JCS	search
	MOVQ	40(R13), R14            // ExceptionInformation[1] fault address
	MOVQ	8(R12), R15             // CONTEXT
	LEAQ	·guardRegions(SB), R10
	MOVQ	$256, R11
scan:
	MOVQ	0(R10), R9
	TESTQ	R9, R9
	JZ	next
	CMPQ	R14, R9
	JCS	next
	MOVQ	8(R10), R9
	CMPQ	R14, R9
	JCC	next
	MOVQ	16(R10), BX             // linMem
	CMPQ	144(R15), BX            // CONTEXT.Rbx
	JNE	next
	MOVQ	R14, AX
	SUBQ	BX, AX
	MOVL	-8(BX), CX
	CMPQ	CX, AX
	JLS	outofbounds

	// Commit the wasm page relative to linMem. linMem is host-page aligned but
	// intentionally not required to share Windows' 64 KiB allocation alignment.
	ANDQ	$-65536, AX
	LEAQ	(BX)(AX*1), AX
	MOVQ	AX, CX                  // allocation-aligned page address
	MOVQ	$65536, DX
	MOVQ	$0x1000, R8             // MEM_COMMIT
	MOVQ	$4, R9                  // PAGE_READWRITE
	SUBQ	$32, SP                 // Windows shadow space
	MOVQ	·guardVirtualAllocPC(SB), AX
	CALL	AX
	ADDQ	$32, SP
	TESTQ	AX, AX
	JNZ	continued
	MOVL	$4, CX
	JMP	settrap
outofbounds:
	MOVL	$3, CX
settrap:
	MOVQ	-104(BX), AX
	TESTQ	AX, AX
	JZ	search
	MOVL	CX, (AX)
	MOVQ	·guardTrapExitHandlerJumpPC(SB), AX
	MOVQ	AX, 248(R15)            // CONTEXT.Rip
continued:
	MOVQ	$-1, AX
	JMP	done
next:
	ADDQ	$32, R10
	DECQ	R11
	JNZ	scan
search:
	XORL	AX, AX
done:
	POPQ	R15
	POPQ	R14
	POPQ	R13
	POPQ	R12
	POPQ	BX
	RET

TEXT ·addrGuardExceptionHandler(SB), NOSPLIT, $0-8
	LEAQ	·guardExceptionHandler(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·nativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	-24(BX), SP
	RET

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT, $0-8
	LEAQ	·nativeTrapExitHandlerJump(SB), AX
	MOVQ	AX, ret+0(FP)
	RET
