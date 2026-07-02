//go:build linux && amd64 && wago_guardpage

#include "textflag.h"

// guardSigHandler is a SA_SIGINFO signal handler (C ABI: DI=signo, SI=*siginfo,
// DX=*ucontext). Pure asm, no Go calls, no g use — runs on the signal alt-stack.
// It derives everything per-fault (no per-call shared state): scan the live
// reservation table for one containing the fault address, confirm the faulting
// frame's linMem matches that reservation, then record a wasm out-of-bounds trap
// in the frame's *trap and redirect the saved RIP to the matching native trap
// exit. Anything else chains to Go's saved handler.
//
// Linux amd64 ucontext: uc_mcontext.gregs at +40; REG_RBP=10 -> +120,
// REG_RBX=11 -> +128, REG_RSP=15 -> +160, REG_RIP=16 -> +168. guardRegion is
// {start@0, end@8, linMem@16}, 32 bytes.
TEXT ·guardSigHandler(SB), NOSPLIT|NOFRAME, $0-0
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

	// addr is inside this reservation. First try the frameless x64 ABI: RBX is
	// pinned to linMem and the trap pointer is at [RSP+0].
	MOVQ	16(R10), R9             // region.linMem
	MOVQ	128(DX), AX             // AX = saved RBX (x64 linMem)
	CMPQ	AX, R9
	JE	match_x64

	// Fall back to the framed amd64 ABI: [RBP-16] is linMem and [RBP-24] is trap.
	MOVQ	120(DX), R15            // R15 = saved RBP (faulting frame)
	MOVQ	-16(R15), CX            // CX = [RBP-16] = saved linMem
	CMPQ	CX, R9
	JNE	next                    // mismatch -> not this reservation's wasm fault
	XORL	R14, R14                // mode 0 = framed amd64
	JMP	matched

match_x64:
	MOVQ	AX, CX                  // CX = linMem
	MOVQ	160(DX), R15            // R15 = saved RSP (frameless frame base)
	MOVQ	$1, R14                 // mode 1 = frameless x64

matched:
	// Fault is in this reservation's wasm memory. Lazily commit a grown-but-
	// uncommitted page (off < current logical size), else trap a genuinely
	// out-of-range access. off = fault(R8) - linMem(CX); curBytes = [linMem-8].
	MOVQ	R8, R12
	SUBQ	CX, R12                 // R12 = off (fault - linMem)
	MOVL	-8(CX), R13             // R13 = curBytes (u32, zero-extended)
	CMPQ	R13, R12
	JLS	dotrap                  // curBytes <= off -> out of range -> trap
	// Commit the 64 KiB wasm page containing the fault, then resume the access.
	MOVQ	R8, DI
	ANDQ	$-65536, DI             // align down to the 64 KiB wasm page
	MOVQ	$65536, SI
	MOVQ	$3, DX                  // PROT_READ|PROT_WRITE
	MOVQ	$10, AX                 // SYS_mprotect
	SYSCALL
	RET                             // -> restorer -> rt_sigreturn: retry (now committed)
dotrap:
	// Confirmed wasm OOB. Record the trap and redirect RIP.
	TESTQ	R14, R14
	JNZ	dotrap_x64
	MOVQ	-24(R15), CX            // CX = [RBP-24] = framed trap pointer
	MOVL	$3, (CX)                // TrapLinMemOutOfBounds
	MOVQ	·guardTrapExitFramedPC(SB), R9
	MOVQ	R9, 168(DX)             // saved RIP = nativeTrapExitFramed
	RET                             // -> restorer -> rt_sigreturn -> nativeTrapExitFramed
dotrap_x64:
	MOVQ	0(R15), CX              // CX = [RSP+0] = frameless trap pointer
	MOVL	$3, (CX)                // TrapLinMemOutOfBounds
	MOVQ	·guardTrapExitHandlerJumpPC(SB), R9
	MOVQ	R9, 168(DX)             // saved RIP = nativeTrapExit
	RET                             // -> restorer -> rt_sigreturn -> nativeTrapExit
next:
	ADDQ	$32, R10                // sizeof(guardRegion)
	DECQ	R11
	JNZ	scan
	MOVQ	·guardOldHandler(SB), AX
	JMP	AX

// guardSigRestorer invokes rt_sigreturn (syscall 15) to restore the (rewritten)
// signal context. Referenced as sa_restorer with SA_RESTORER.
TEXT ·guardSigRestorer(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$15, AX
	SYSCALL

// nativeTrapExitFramed is the old framed-backend `leave; ret` landing pad. It
// unwinds exactly one wasm frame and relies on the framed amd64 backend's
// post-call trap propagation.
TEXT ·nativeTrapExitFramed(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	BP, SP                  // leave: rsp = rbp
	MOVQ	0(SP), BP               // leave: pop rbp (restore caller frame ptr)
	ADDQ	$8, SP
	RET                             // return one frame up (to caller's post-call check)

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

TEXT ·addrNativeTrapExitFramed(SB), NOSPLIT, $0-8
	LEAQ	·nativeTrapExitFramed(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT, $0-8
	LEAQ	·nativeTrapExitHandlerJump(SB), AX
	MOVQ	AX, ret+0(FP)
	RET
