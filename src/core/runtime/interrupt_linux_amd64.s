//go:build linux && amd64 && !tinygo

#include "textflag.h"

// SA_SIGINFO handler: DI=signal, SI=*siginfo, DX=*ucontext.
// Linux amd64 ucontext has saved RBX at +128, RSP at +160, and RIP at +168.
// Generated Wasm pins linMem in RBX; [linMem-104] is its active trap pointer.
// interruptRequest is {trap uintptr, ack u32, refs u32}, 16 bytes.
TEXT ·interruptSigHandler(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	DX, R8
	CMPL	8(SI), $-6                  // only Wago's tgkill broadcast is ours
	JE	check_pc
	CMPL	8(SI), $-2                  // per-thread deadline timer
	JE	check_pc
	MOVQ	·interruptOldHandler(SB), AX
	TESTQ	AX, AX
	JNZ	chain_old_handler
handler_return:
	RET
chain_old_handler:
	JMP	AX                           // preserve Go/os-signal behavior

check_pc:
	MOVQ	168(R8), R11                // saved RIP
	LEAQ	·executableCodeRanges(SB), R12
	MOVL	·executableCodeRangeLimit(SB), R13
	TESTQ	R13, R13
	JZ	handler_return
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
	RET

interrupt_match:
	MOVQ	128(R8), AX                 // saved RBX = linMem
	TESTQ	AX, AX
	JZ	handler_return
	MOVQ	-104(AX), R10               // active trap pointer
	LEAQ	·interruptRequests(SB), R9
	MOVQ	$64, R12
request_loop:
	CMPQ	0(R9), R10
	JE	request_match
	ADDQ	$16, R9
	DECQ	R12
	JNZ	request_loop
	RET
request_match:
	MOVL	$12, 0(R10)                 // TrapInterrupted
	MOVL	$1, 8(R9)                   // acknowledgement
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
