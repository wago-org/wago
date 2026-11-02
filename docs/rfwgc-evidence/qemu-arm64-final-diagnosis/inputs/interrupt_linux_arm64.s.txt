//go:build linux && arm64 && !tinygo && !wago_target_tinygo

#include "textflag.h"

// SA_SIGINFO handler: R0=signal, R1=*siginfo, R2=*ucontext.
// Linux arm64 ucontext has saved X9 at +256, X26 at +392, SP at +432, and
// PC at +440. Generated Wasm pins linMem in X26; [linMem-104] is its active
// trap pointer. interruptRequest is {trap uintptr, ack u32, refs u32, token u64}, 24 bytes.
TEXT ·interruptSigHandler(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	R0, R3                      // preserve signal arguments for chaining
	MOVD	R1, R4
	MOVD	R2, R5
	MOVD	R2, R7                      // saved ucontext
	MOVW	8(R4), R11
	CMPW	$-1, R11                   // SI_QUEUE
	BEQ	check_cookie
	CMPW	$-2, R11                   // SI_TIMER
	BNE	chain_unowned
check_cookie:
	MOVWU	28(R4), R11
	MOVD	$·interruptCookie(SB), R12
	LDARW	(R12), R12
	CMP	R11, R12
	BEQ	check_pc
chain_unowned:
	MOVD	·interruptOldHandler(SB), R11
	CMP	$1, R11
	BLS	handler_return
	MOVD	R3, R0
	MOVD	R4, R1
	MOVD	R5, R2
	B	(R11)                       // preserve Go/os-signal behavior
handler_return:
	RET

check_pc:
	MOVD	440(R7), R11               // saved PC
	MOVD	$·executableCodeRanges(SB), R12
	MOVD	$·executableCodeRangeLimit(SB), R13
	LDARW	(R13), R13
	CBZ	R13, handler_return
range_loop:
	LDAR	(R12), R14                  // acquire range.start before range.end
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
	RET

interrupt_match:
	MOVD	392(R7), R11               // saved X26 = linMem
	CBZ	R11, handler_return
	MOVD	$·interruptLinearMemoryState(SB), R17
reader_acquire:
	LDAXRW	(R17), R14
	TBNZ	$31, R14, handler_return    // Close has blocked new registry readers
	ADDW	$1, R14, R15
	STLXRW	R15, (R17), R13
	CBNZ	R13, reader_acquire
	MOVD	$·interruptLinearMemories(SB), R9
	MOVD	$·interruptLinearMemoryLimit(SB), R12
	LDARW	(R12), R12
	CBZ	R12, reader_release
linmem_loop:
	LDAR	(R9), R13
	CMP	R13, R11
	BEQ	linmem_match
	ADD	$8, R9
	SUB	$1, R12
	CBNZ	R12, linmem_loop
	B	reader_release
linmem_match:
	MOVD	-104(R11), R10             // active trap pointer
	MOVD	$·interruptRequests(SB), R9
	MOVD	$64, R12
request_loop:
	LDAR	(R9), R13                   // acquire trap publication before token
	CMP	R13, R10
	BNE	request_next
	MOVD	24(R4), R14
	ADD	$16, R9, R13
	LDAR	(R13), R13
	CMP	R13, R14
	BEQ	request_match
request_next:
	ADD	$24, R9
	SUB	$1, R12
	CBNZ	R12, request_loop
	B	reader_release
request_match:
	MOVW	$12, R16
	STLRW	R16, (R10)                 // TrapInterrupted
	MOVW	$1, R16
	ADD	$8, R9, R13
	STLRW	R16, (R13)                 // publish acknowledgement after trap
	MOVD	R11, 256(R7)               // saved X9 = linMem for landing pad
	MOVD	·interruptTrapPC(SB), R11
	MOVD	R11, 440(R7)               // saved PC = landing pad
	B	reader_release
reader_release:
	LDAXRW	(R17), R14
	SUBW	$1, R14, R15
	STLXRW	R15, (R17), R13
	CBNZ	R13, reader_release
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
