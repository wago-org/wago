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
	MOVQ	16(R10), BX             // region.linMem (fault-address base)
	MOVQ	24(R10), AX             // region.ownerLinMem
	CMPQ	144(R15), AX            // CONTEXT.Rbx is the primary linMem
	JNE	next
	MOVQ	R14, AX
	SUBQ	BX, AX
	MOVQ	-288(BX), CX
	CMPQ	CX, AX
	JLS	outofbounds

	// Arrange a return through guardCommitPage. Allocating directly inside VEH
	// leaves the reservation inaccessible on native Windows; the thunk runs only
	// after exception dispatch has restored ordinary execution.
	ANDQ	$-65536, AX
	LEAQ	(BX)(AX*1), AX
	MOVQ	152(R15), R11           // saved RSP
	SUBQ	$40, R11                // page + padding + retry PC
	MOVQ	AX, 0(R11)
	// Windows uses R8 while transferring control to the continuation thunk.
	// Carry the faulting value explicitly so the thunk can restore the exact
	// register file before retrying the memory instruction.
	MOVQ	184(R15), AX            // CONTEXT.R8
	MOVQ	AX, 8(R11)
	MOVQ	248(R15), AX            // faulting RIP
	MOVQ	AX, 32(R11)             // RET target after the commit
	MOVQ	R11, 152(R15)
	LEAQ	·guardCommitPage(SB), AX
	MOVQ	AX, 248(R15)
	JMP	continued
outofbounds:
	MOVL	$3, CX
settrap:
	MOVQ	144(R15), AX            // active primary linMem
	MOVQ	-104(AX), AX
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

// guardCommitPage runs outside exception dispatch with the faulting register
// file restored. Preserve every Windows-volatile register the compiler can keep
// live across a memory access, commit one Wasm page, then RET to the faulting
// instruction saved by VEH. Entry SP points at {page, pad[3], retryPC}.
TEXT ·guardCommitPage(SB), NOSPLIT|NOFRAME, $0-0
	LEAQ	-200(SP), SP            // 32 shadow + GP/flags + XMM0..5; call-aligned
	MOVQ	AX, 32(SP)
	MOVQ	CX, 40(SP)
	MOVQ	DX, 48(SP)
	MOVQ	R8, 56(SP)
	MOVQ	R9, 64(SP)
	MOVQ	R10, 72(SP)
	MOVQ	R11, 80(SP)
	PUSHFQ
	POPQ	AX
	MOVQ	AX, 88(SP)
	MOVQ	208(SP), AX            // faulting R8 carried by the VEH frame
	MOVQ	AX, 56(SP)             // replace Windows' continuation-time R8
	MOVOU	X0, 96(SP)
	MOVOU	X1, 112(SP)
	MOVOU	X2, 128(SP)
	MOVOU	X3, 144(SP)
	MOVOU	X4, 160(SP)
	MOVOU	X5, 176(SP)
	MOVQ	200(SP), CX            // allocation-aligned page from VEH frame
	MOVQ	$65536, DX
	MOVQ	$0x1000, R8            // MEM_COMMIT
	MOVQ	$4, R9                 // PAGE_READWRITE
	MOVQ	·guardVirtualAllocPC(SB), AX
	CALL	AX
	TESTQ	AX, AX
	JZ	commitfailed
	MOVOU	96(SP), X0
	MOVOU	112(SP), X1
	MOVOU	128(SP), X2
	MOVOU	144(SP), X3
	MOVOU	160(SP), X4
	MOVOU	176(SP), X5
	MOVQ	88(SP), AX
	PUSHQ	AX
	POPFQ
	MOVQ	32(SP), AX
	MOVQ	40(SP), CX
	MOVQ	48(SP), DX
	MOVQ	56(SP), R8
	MOVQ	64(SP), R9
	MOVQ	72(SP), R10
	MOVQ	80(SP), R11
	LEAQ	232(SP), SP             // frame + header before retryPC; keep flags
	RET
commitfailed:
	MOVQ	-104(BX), AX
	TESTQ	AX, AX
	JZ	commitsearch
	MOVL	$4, (AX)                // TrapLinMemCouldNotExtend
	JMP	·nativeTrapExitHandlerJump(SB)
commitsearch:
	INT	$3                      // impossible without an active guarded call

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
