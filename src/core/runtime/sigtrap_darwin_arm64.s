//go:build darwin && arm64 && wago_guardpage

#include "textflag.h"

TEXT ·libcSigactionTrampoline(SB), NOSPLIT, $0-0
	JMP	libc_sigaction(SB)

TEXT ·addrLibcSigactionTrampoline(SB), NOSPLIT, $0-8
	MOVD	$·libcSigactionTrampoline(SB), R0
	MOVD	R0, ret+0(FP)
	RET

// Darwin arm64 SA_SIGINFO handler: R0=signo, R1=*siginfo, R2=*ucontext.
// SDK-verified offsets:
//   siginfo.si_addr       = +24
//   ucontext.uc_mcontext  = +48
//   mcontext64.ss.x[26]   = +224
//   mcontext64.ss.pc      = +272
//   mcontext64.ss.flags   = +284
// guardRegion is {start@0, end@8, linMem@16}, 32 bytes.
TEXT ·guardSigHandler(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	R0, R3                  // preserve signal arguments for chaining
	MOVD	R1, R4
	MOVD	R2, R5
	MOVD	24(R1), R8              // R8 = fault address
	MOVD	$·guardRegions(SB), R10 // R10 = &guardRegions[0]
	MOVD	$256, R11               // R11 = slots remaining
scan:
	LDAR	(R10), R9               // acquire region.start publication
	CBZ	R9, next
	CMP	R9, R8
	BLO	next                    // addr < start
	MOVD	8(R10), R9              // region.end
	CMP	R9, R8
	BHS	next                    // addr >= end

	MOVD	16(R10), R9             // region.linMem
	MOVD	48(R2), R14             // ucontext.uc_mcontext
	MOVD	224(R14), R26           // saved X26 (pinned linMem)
	CMP	R9, R26
	BNE	next                    // not this reservation's wasm fault

	// off = fault - linMem; curBytes = [linMem-8].
	MOVD	R8, R12
	SUB	R26, R12                // R12 = fault - linMem
	MOVWU	-8(R26), R13            // logical linear-memory size
	CMP	R13, R12
	BHS	outofbounds             // curBytes <= off

	// Grown-but-uncommitted page. Commit the containing 64 KiB wasm page and
	// return through libSystem's signal trampoline so the access is retried.
	MOVD	R8, R0
	AND	$-65536, R0             // page-aligned address
	MOVD	$65536, R1
	MOVD	$3, R2                  // PROT_READ|PROT_WRITE
	MOVD	$74, R16                // SYS_mprotect
	SVC	$0x80
	BCC	resume
	MOVW	$4, R13                 // TrapLinMemCouldNotExtend
	B	settrap
resume:
	RET

outofbounds:
	MOVW	$3, R13                 // TrapLinMemOutOfBounds
settrap:
	MOVD	-104(R26), R12          // basedata trap-cell pointer
	CBZ	R12, chain               // no active guarded call: not ours
	MOVW	R13, (R12)
	MOVD	·guardTrapExitHandlerJumpPC(SB), R9
	MOVD	R9, 272(R14)            // saved PC = native trap-exit landing pad
	MOVWU	284(R14), R9
	ORR	$1, R9                  // NO_PTRAUTH for replacement PC
	MOVW	R9, 284(R14)
	RET

next:
	ADD	$32, R10
	SUB	$1, R11
	CBNZ	R11, scan

chain:
	// SIGBUS is 10 and SIGSEGV is 11 on Darwin. Preserve R0/R1/R2 so the old
	// SA_SIGINFO handler receives the original signal arguments.
	MOVD	R3, R0
	MOVD	R4, R1
	MOVD	R5, R2
	CMPW	$10, R0
	BEQ	chainbus
	MOVD	·guardOldSEGVHandler(SB), R9
	B	(R9)
chainbus:
	MOVD	·guardOldBUSHandler(SB), R9
	B	(R9)

TEXT ·addrGuardSigHandler(SB), NOSPLIT, $0-8
	MOVD	$·guardSigHandler(SB), R0
	MOVD	R0, ret+0(FP)
	RET

// nativeTrapExitHandlerJump restores the foreign-stack save-area pointer and
// branches to enterNative's continuation. X26 remains the faulting linMem.
TEXT ·nativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	-24(R26), R9
	MOVD	R9, RSP
	MOVD	-32(R26), R9
	B	(R9)

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT, $0-8
	MOVD	$·nativeTrapExitHandlerJump(SB), R0
	MOVD	R0, ret+0(FP)
	RET
