//go:build linux && arm64 && !tinygo

#include "textflag.h"

// SA_SIGINFO handler: R0=signal, R1=*siginfo, R2=*ucontext.
// Linux arm64 ucontext has saved X9 at +256, X26 at +392, SP at +432, and
// PC at +440. interruptActivation is 40 bytes; state/tid/trap/ack/linMem
// occupy offsets 0/4/8/16/32.
TEXT ·interruptSigHandler(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	R0, R3                      // preserve signal arguments for chaining
	MOVD	R1, R4
	MOVD	R2, R5
	MOVD	R2, R7                      // saved ucontext
	MOVD	$178, R8                    // SYS_gettid
	SVC
	MOVD	$·interruptActivations(SB), R9
	MOVD	$64, R10
activation_loop:
	MOVWU	4(R9), R11
	CMPW	R11, R0
	BNE	activation_next
	MOVWU	0(R9), R11
	CMPW	$2, R11                    // interruptWasm
	BEQ	activation_match
	MOVW	8(R4), R11
	CMPW	$-2, R11                   // SI_TIMER: preserve deadline across host park
	BNE	runtime_miss
	MOVD	8(R9), R11
	MOVW	$12, R16
	MOVW	R16, (R11)                 // TrapInterrupted, consumed at host boundary
runtime_miss:
	MOVW	ZR, 16(R9)                 // delivery missed Wasm; allow retry
	RET
activation_next:
	ADD	$40, R9
	SUB	$1, R10
	CBNZ	R10, activation_loop
	MOVW	8(R4), R11
	CMPW	$-6, R11                   // SI_TKILL raced activation teardown
	BEQ	handler_return
	CMPW	$-2, R11                   // SI_TIMER after timer deletion/exit
	BEQ	handler_return
	MOVD	·interruptOldHandler(SB), R11
	CBZ	R11, handler_return
	MOVD	R3, R0
	MOVD	R4, R1
	MOVD	R5, R2
	B	(R11)                       // preserve Go/os-signal behavior
handler_return:
	RET

activation_match:
	MOVD	440(R7), R11               // saved PC
	MOVD	$·executableCodeRanges(SB), R12
	MOVWU	·executableCodeRangeLimit(SB), R13
	CBZ	R13, range_miss
range_loop:
	MOVD	0(R12), R14                // range.start
	CBZ	R14, range_next
	CMP	R14, R11
	BLO	range_next                  // PC < start
	MOVD	8(R12), R15                // range.end
	CMP	R15, R11
	BHS	range_next                  // PC >= end
	B	interrupt_match
range_next:
	ADD	$16, R12
	SUB	$1, R13
	CBNZ	R13, range_loop
range_miss:
	MOVW	ZR, 16(R9)                 // host/runtime PC; allow retry on re-entry
	RET

interrupt_match:
	MOVW	$3, R16
	MOVW	R16, 0(R9)                 // interruptUnwinding: reject duplicates
	MOVD	8(R9), R11                 // activation.trap
	MOVW	$12, R16
	MOVW	R16, (R11)                 // TrapInterrupted
	MOVW	$1, R16
	MOVW	R16, 16(R9)                // acknowledgement
	MOVD	32(R9), R11                // activation.linMem
	MOVD	R11, 256(R7)               // saved X9 = linMem for landing pad
	MOVD	·interruptTrapPC(SB), R11
	MOVD	R11, 440(R7)               // saved PC = landing pad
	RET

// The rewritten context resumes with X9 naming active linear memory. The
// trampoline saved its foreign-stack save area and continuation below linMem.
TEXT ·nativeInterruptTrap(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	-24(R9), R10
	MOVD	R10, RSP
	MOVD	-32(R9), R10
	B	(R10)

TEXT ·addrInterruptSigHandler(SB), NOSPLIT, $0-8
	MOVD	$·interruptSigHandler(SB), R0
	MOVD	R0, ret+0(FP)
	RET

TEXT ·addrNativeInterruptTrap(SB), NOSPLIT, $0-8
	MOVD	$·nativeInterruptTrap(SB), R0
	MOVD	R0, ret+0(FP)
	RET
