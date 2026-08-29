//go:build linux && amd64 && wago_guardpage

#include "textflag.h"

// guardSigHandler is a SA_SIGINFO signal handler (C ABI: DI=signo, SI=*siginfo,
// DX=*ucontext). Pure asm, no Go calls, no g use — runs on the signal alt-stack.
// It derives everything per-fault (no per-call shared state): scan the live
// reservation table for one containing the fault address, confirm the faulting
// frame's pinned linMem owns that reservation, then record a wasm out-of-bounds trap
// in the frame's *trap and redirect the saved RIP to the matching native trap
// exit. Anything else chains to Go's saved handler.
//
// Linux amd64 ucontext: uc_mcontext.gregs at +40; REG_RBP=10 -> +120,
// REG_RBX=11 -> +128, REG_RSP=15 -> +160, REG_RIP=16 -> +168. guardRegion is
// {start@0, end@8, linMem@16, ownerLinMem@24}, 32 bytes.
TEXT ·guardSigHandler(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	DX, R12                 // preserve *ucontext across mprotect arguments
	MOVQ	16(SI), R8              // R8 = siginfo->si_addr (fault address)
	LEAQ	·guardRegions(SB), R10  // R10 = &guardRegions[0]
	MOVQ	$256, R11               // R11 = slots left (maxGuardRegions)
scan:
	MOVQ	0(R10), R9              // region.start
	TESTQ	R9, R9
	JZ	next                    // free slot
	CMPQ	R8, R9
	JCS	next                    // addr < start
	MOVQ	8(R10), R9              // region.end
	CMPQ	R8, R9
	JCC	next                    // addr >= end

	// addr is inside this reservation. The x64 ABI pins linMem in RBX and keeps
	// the trap cell pointer at [linMem-TrapCellPtrOffset]. A mismatched RBX chains
	// without dereferencing arbitrary frame memory.
	MOVQ	16(R10), R9             // region.linMem (fault-address base)
	MOVQ	24(R10), R13            // region.ownerLinMem (active primary)
	MOVQ	128(R12), AX            // AX = saved RBX (x64 primary linMem)
	CMPQ	AX, R13
	JNE	next
	MOVQ	AX, CX                  // CX = linMem
	// Fault is in this reservation's wasm memory. Lazily commit a grown-but-
	// uncommitted page (off < current logical size), else trap a genuinely
	// out-of-range access. R9 is the faulting memory base; CX is the active
	// primary linMem used for trap/unwind state.
	MOVQ	R8, AX
	SUBQ	R9, AX                  // AX = off (fault - region.linMem)
	MOVQ	-288(R9), R13           // R13 = authoritative region curBytes (u64)
	CMPQ	R13, AX
	JLS	dotrap                  // curBytes <= off -> out of range -> trap
	// Commit the complete 64 KiB wasm page containing the fault. Align the
	// reservation-relative offset, not the absolute address: mmap guarantees
	// linMem is host-page aligned but not necessarily 64-KiB aligned.
	MOVQ	CX, R10                 // SYSCALL clobbers CX; retain primary linMem
	ANDQ	$-65536, AX             // wasm-page-aligned offset from region.linMem
	LEAQ	(R9)(AX*1), DI
	MOVQ	$65536, SI
	MOVQ	$3, DX                  // PROT_READ|PROT_WRITE
	MOVQ	$10, AX                 // SYS_mprotect
	SYSCALL
	TESTQ	AX, AX
	JGE	resume
	MOVQ	R10, CX                 // restore primary linMem for trap publication
	MOVL	$4, R13                 // TrapLinMemCouldNotExtend
	JMP	settrap
resume:
	RET                             // -> restorer -> rt_sigreturn: retry (now committed)
dotrap:
	MOVL	$3, R13                 // TrapLinMemOutOfBounds
settrap:
	// Record the selected wasm trap and redirect RIP.
	// The trap cell moved off the stack into basedata: [linMem-TrapCellPtrOffset]
	// holds the trap-cell POINTER (CX is still linMem here) and the code is written
	// through it, exactly as emitTrap does. (It was [RSP+0] under the pre-basedata
	// ABI.) TrapCellPtrOffset is asserted == 104 in sigtrap_linux_amd64.go.
	MOVQ	-104(CX), CX            // CX = trap cell pointer = [linMem - 104]
	MOVL	R13, (CX)
	MOVQ	·guardTrapExitHandlerJumpPC(SB), R9
	MOVQ	R9, 168(R12)            // saved RIP = nativeTrapExitHandlerJump
	RET                             // -> restorer -> rt_sigreturn -> nativeTrapExit
next:
	ADDQ	$32, R10                // sizeof(guardRegion)
	DECQ	R11
	JNZ	scan
	CMPL	DI, $7                  // SIGBUS=7, SIGSEGV=11 on Linux
	JE	chainbus
	MOVQ	·guardOldSEGVHandler(SB), AX
	JMP	AX
chainbus:
	MOVQ	·guardOldBUSHandler(SB), AX
	JMP	AX

// guardSigRestorer invokes rt_sigreturn (syscall 15) to restore the (rewritten)
// signal context. Referenced as sa_restorer with SA_RESTORER.
TEXT ·guardSigRestorer(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$15, AX
	SYSCALL

// nativeTrapExitHandlerJump is the x64/WARP landing pad. RBX is still the
// faulting frame's linMem after sigreturn; [RBX-24] is the trampoline-recorded
// re-entry SP. Restoring it and RETing jumps straight back to enterNative.
TEXT ·nativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	-24(BX), SP
	RET

TEXT ·addrGuardSigHandler(SB), NOSPLIT, $0-8
	LEAQ	·guardSigHandler(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·addrGuardSigRestorer(SB), NOSPLIT, $0-8
	LEAQ	·guardSigRestorer(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT, $0-8
	LEAQ	·nativeTrapExitHandlerJump(SB), AX
	MOVQ	AX, ret+0(FP)
	RET
