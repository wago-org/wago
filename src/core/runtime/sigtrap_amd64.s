//go:build linux && amd64 && wago_guardpage

#include "textflag.h"

// guardSigHandler is a SA_SIGINFO signal handler (C ABI: DI=signo, SI=*siginfo,
// DX=*ucontext). Pure asm, no Go calls, no g use — runs on the signal alt-stack.
// If the fault address is inside the linear-memory reservation it records a wasm
// out-of-bounds trap and rewrites only the saved RIP so the signal returns into
// nativeTrapExit (a leave;ret on the faulting frame); otherwise it chains to
// Go's saved handler.
TEXT ·guardSigHandler(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	16(SI), R8              // R8 = siginfo->si_addr (fault address)
	MOVQ	·guardReserveStart(SB), R9
	CMPQ	R8, R9
	JCS	chain                   // below start (unsigned) -> not ours
	MOVQ	·guardReserveEnd(SB), R9
	CMPQ	R8, R9
	JCC	chain                   // >= end (unsigned) -> not ours
	// Inside the reservation: wasm OOB. Write TrapLinMemOutOfBounds (3) to *trap.
	MOVQ	·guardTrapPtr(SB), R9
	MOVL	$3, (R9)
	// Redirect only the saved RIP to nativeTrapExit, leaving RSP/RBP at the
	// faulting frame. Linux amd64 ucontext: uc_mcontext.gregs at +40, REG_RIP=16
	// -> +168. nativeTrapExit runs `leave; ret` on the faulting frame, unwinding
	// one wasm frame into wago's normal trap-propagation path.
	MOVQ	·guardTrapExitPC(SB), R9
	MOVQ	R9, 168(DX)
	RET                             // -> restorer -> rt_sigreturn -> nativeTrapExit
chain:
	MOVQ	·guardOldHandler(SB), AX
	JMP	AX

// guardSigRestorer invokes rt_sigreturn (syscall 15) to restore the (rewritten)
// signal context. Referenced as sa_restorer with SA_RESTORER.
TEXT ·guardSigRestorer(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$15, AX
	SYSCALL

// nativeTrapExit is a `leave; ret` the handler points the faulting wasm frame's
// RIP at. It unwinds exactly one wasm frame (RSP/RBP are still the faulting
// frame's), landing either at the caller's post-call trap check (which then
// propagates the trap up via the same path) or, for the entry frame, back in
// enterNative's epilogue — wago's existing trap unwind, reached without a check.
TEXT ·nativeTrapExit(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	BP, SP                  // leave: rsp = rbp
	MOVQ	0(SP), BP               // leave: pop rbp (restore caller frame ptr)
	ADDQ	$8, SP
	RET                             // return one frame up (to caller's post-call check)

TEXT ·addrGuardSigHandler(SB), NOSPLIT, $0-8
	LEAQ	·guardSigHandler(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·addrGuardSigRestorer(SB), NOSPLIT, $0-8
	LEAQ	·guardSigRestorer(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·addrNativeTrapExit(SB), NOSPLIT, $0-8
	LEAQ	·nativeTrapExit(SB), AX
	MOVQ	AX, ret+0(FP)
	RET
