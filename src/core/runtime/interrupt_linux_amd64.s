//go:build linux && amd64 && !tinygo

#include "textflag.h"

// SA_SIGINFO handler: DI=signal, SI=*siginfo, DX=*ucontext.
// Linux amd64 ucontext has saved RBX at +128, RSP at +160, and RIP at +168.
// interruptActivation is 32 bytes; state/tid/trap/ack occupy offsets 0/4/8/16.
TEXT ·interruptSigHandler(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	DX, R8
	MOVQ	$186, AX                    // SYS_gettid
	SYSCALL
	LEAQ	·interruptActivations(SB), R9
	MOVQ	$64, R10
activation_loop:
	CMPL	4(R9), AX
	JNE	activation_next
	CMPL	0(R9), $2                   // interruptWasm
	JE	activation_match
	CMPL	8(SI), $-2                  // SI_TIMER: preserve deadline across host park
	JNE	runtime_miss
	MOVQ	8(R9), AX
	MOVL	$12, 0(AX)                  // TrapInterrupted, consumed at host boundary
runtime_miss:
	MOVL	$0, 16(R9)                  // delivery missed Wasm; allow retry
	RET	                                // our target is in runtime/entry/exit
activation_next:
	ADDQ	$32, R9
	DECQ	R10
	JNZ	activation_loop
	CMPL	8(SI), $-6                  // SI_TKILL: Wago delivery raced activation
	JE	handler_return
	CMPL	8(SI), $-2                  // SI_TIMER after timer deletion/activation exit
	JE	handler_return
	MOVQ	·interruptOldHandler(SB), AX
	TESTQ	AX, AX
	JNZ	chain_old_handler
handler_return:
	RET
chain_old_handler:
	JMP	AX                           // preserve Go/os-signal behavior

activation_match:
	MOVQ	168(R8), R11                // saved RIP
	LEAQ	·executableCodeRanges(SB), R12
	MOVL	·executableCodeRangeLimit(SB), R13
	TESTQ	R13, R13
	JZ	range_miss
range_loop:
	MOVQ	0(R12), R14                 // range.start
	TESTQ	R14, R14
	JZ	range_next
	CMPQ	R11, R14
	JCS	range_next
	MOVQ	8(R12), R15                 // range.end
	CMPQ	R11, R15
	JCS	interrupt_match
range_next:
	ADDQ	$16, R12
	DECQ	R13
	JNZ	range_loop
range_miss:
	MOVL	$0, 16(R9)                  // host/runtime PC; allow retry on re-entry
	RET

interrupt_match:
	MOVL	$3, 0(R9)                   // interruptUnwinding: reject duplicates
	MOVQ	8(R9), AX                   // activation.trap
	MOVL	$12, 0(AX)                  // TrapInterrupted
	MOVL	$1, 16(R9)                  // acknowledgement
	MOVQ	128(R8), AX                 // saved RBX = active linMem
	MOVQ	-24(AX), AX                 // trampoline's trap re-entry RSP
	MOVQ	AX, 160(R8)                 // saved RSP
	MOVQ	·interruptTrapPC(SB), AX
	MOVQ	AX, 168(R8)                 // saved RIP = one-instruction landing pad
	RET

TEXT ·interruptSigRestorer(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	$15, AX                     // SYS_rt_sigreturn
	SYSCALL

// The rewritten context resumes here on the preallocated foreign stack.
// RSP names enterNative's parked return address, so RET jumps to its normal
// Go-context restoration epilogue.
TEXT ·nativeInterruptTrap(SB), NOSPLIT|NOFRAME, $0-0
	RET

TEXT ·addrInterruptSigHandler(SB), NOSPLIT, $0-8
	LEAQ	·interruptSigHandler(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·addrInterruptSigRestorer(SB), NOSPLIT, $0-8
	LEAQ	·interruptSigRestorer(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·addrNativeInterruptTrap(SB), NOSPLIT, $0-8
	LEAQ	·nativeInterruptTrap(SB), AX
	MOVQ	AX, ret+0(FP)
	RET
