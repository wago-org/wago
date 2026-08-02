//go:build darwin && amd64 && wago_guardpage

#include "textflag.h"

TEXT ·libcSigactionTrampoline(SB), NOSPLIT, $0-0
	JMP	libc_sigaction(SB)

TEXT ·addrLibcSigactionTrampoline(SB), NOSPLIT, $0-8
	LEAQ	·libcSigactionTrampoline(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

// Darwin amd64 SA_SIGINFO handler: DI=signo, SI=*siginfo, DX=*ucontext.
// SDK-verified offsets:
//   siginfo.si_addr       = +24
//   ucontext.uc_mcontext  = +48
//   mcontext64.ss.rbx     = +24 (16-byte exception state + 8)
//   mcontext64.ss.rip     = +144 (16-byte exception state + 128)
// guardRegion is {start@0, end@8, linMem@16}, 32 bytes.
TEXT ·guardSigHandler(SB), NOSPLIT|NOFRAME, $0-0
	// Preserve every callee-saved register we use. libSystem's signal trampoline
	// is an ordinary C caller even though sigreturn later restores the faulting
	// register image.
	SUBQ	$40, SP
	MOVQ	BX, 0(SP)
	MOVQ	R12, 8(SP)
	MOVQ	R13, 16(SP)
	MOVQ	R14, 24(SP)
	MOVQ	R15, 32(SP)
	MOVQ	DI, R12                 // preserve signal arguments for chaining
	MOVQ	SI, R13
	MOVQ	DX, R14
	MOVQ	24(SI), R8              // fault address
	LEAQ	·guardRegions(SB), R10
	MOVQ	$256, R11
scan:
	MOVQ	0(R10), R9              // acquire on x86 (TSO)
	TESTQ	R9, R9
	JZ	next
	CMPQ	R8, R9
	JCS	next                    // addr < start
	MOVQ	8(R10), R9
	CMPQ	R8, R9
	JCC	next                    // addr >= end

	MOVQ	16(R10), BX             // region.linMem
	MOVQ	48(R14), R15            // ucontext.uc_mcontext
	CMPQ	24(R15), BX             // saved RBX is the pinned linMem
	JNE	next

	MOVQ	R8, AX
	SUBQ	BX, AX                  // fault - linMem
	MOVL	-8(BX), CX              // current logical byte size
	CMPQ	CX, AX
	JLS	outofbounds             // curBytes <= off

	// Commit a grown-but-uncommitted 64 KiB wasm page and retry the access.
	MOVQ	R8, DI
	ANDQ	$-65536, DI
	MOVQ	$65536, SI
	MOVQ	$3, DX                  // PROT_READ|PROT_WRITE
	// Use libSystem rather than issuing SYSCALL directly. Rosetta's signal
	// context bookkeeping requires its libc transition when an x86 handler
	// changes page protection.
	CALL	libc_mprotect(SB)
	TESTQ	AX, AX
	JZ	resume
	MOVL	$4, CX                  // TrapLinMemCouldNotExtend
	JMP	settrap
resume:
	MOVQ	32(SP), R15
	MOVQ	24(SP), R14
	MOVQ	16(SP), R13
	MOVQ	8(SP), R12
	MOVQ	0(SP), BX
	ADDQ	$40, SP
	RET

outofbounds:
	MOVL	$3, CX                  // TrapLinMemOutOfBounds
settrap:
	MOVQ	-104(BX), AX            // basedata trap-cell pointer
	TESTQ	AX, AX
	JZ	chain
	MOVL	CX, (AX)
	MOVQ	·guardTrapExitHandlerJumpPC(SB), AX
	MOVQ	AX, 144(R15)            // saved RIP = native trap-exit landing pad
	MOVQ	32(SP), R15
	MOVQ	24(SP), R14
	MOVQ	16(SP), R13
	MOVQ	8(SP), R12
	MOVQ	0(SP), BX
	ADDQ	$40, SP
	RET

next:
	ADDQ	$32, R10
	DECQ	R11
	JNZ	scan

chain:
	MOVQ	R12, DI
	MOVQ	R13, SI
	MOVQ	R14, DX
	MOVQ	32(SP), R15
	MOVQ	24(SP), R14
	MOVQ	16(SP), R13
	MOVQ	8(SP), R12
	MOVQ	0(SP), BX
	ADDQ	$40, SP
	MOVQ	$10, AX                 // SIGBUS
	CMPQ	DI, AX
	JEQ	chainbus
	MOVQ	·guardOldSEGVHandler(SB), AX
	JMP	AX
chainbus:
	MOVQ	·guardOldBUSHandler(SB), AX
	JMP	AX

TEXT ·addrGuardSigHandler(SB), NOSPLIT, $0-8
	LEAQ	·guardSigHandler(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·nativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	-24(BX), SP
	RET

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT, $0-8
	LEAQ	·nativeTrapExitHandlerJump(SB), AX
	MOVQ	AX, ret+0(FP)
	RET
