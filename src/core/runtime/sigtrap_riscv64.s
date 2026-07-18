//go:build linux && riscv64 && wago_guardpage

#include "textflag.h"

// Linux/riscv64 SA_SIGINFO signal ABI:
//   A0 = signal, A1 = *siginfo, A2 = *ucontext
//   siginfo.si_addr = +16
//   ucontext.uc_mcontext = +176
//   saved PC = +176
//   saved S9 = +176 + 25*8 = +376
// guardRegion is {start@0, end@8, linMem@16}, 32 bytes.
TEXT ·guardSigHandler(SB), NOSPLIT|NOFRAME, $0-0
	MOV	16(A1), T0                   // fault address
	MOV	$·guardRegions(SB), T1
	MOV	$256, T2
scan:
	MOV	0(T1), T3                    // region.start
	BEQ	T3, ZERO, next
	FENCE                                  // acquire end/linMem after published start
	BLTU	T0, T3, next                 // fault < start
	MOV	8(T1), T3                    // region.end
	BGEU	T0, T3, next                 // fault >= end

	MOV	16(T1), T3                   // region.linMem
	MOV	376(A2), T4                  // faulting frame's saved S9/linMem
	BNE	T3, T4, next

	SUB	T4, T0, T5                  // offset = fault - linMem
	MOVWU	-8(T4), T6                  // current logical byte size
	BGEU	T5, T6, dotrap               // offset >= size: real wasm OOB

	// The fault is within memory.grow's logical size but its page has not been
	// committed yet. Commit the containing 64 KiB wasm page and retry.
	SRLI	$16, T0, A0
	SLLI	$16, A0, A0                  // page-aligned address
	MOV	$65536, A1
	MOV	$3, A2                       // PROT_READ|PROT_WRITE
	MOV	$226, A7                     // SYS_mprotect
	ECALL
	RET

dotrap:
	MOV	-104(T4), T5                 // basedata trap-cell pointer
	MOV	$3, T6                       // TrapLinMemOutOfBounds
	MOVW	T6, 0(T5)
	MOV	·guardTrapExitPC(SB), T3
	MOV	T3, 176(A2)                  // rewrite saved PC
	RET

next:
	ADD	$32, T1, T1
	SUB	$1, T2, T2
	BNE	T2, ZERO, scan

	// Chain faults outside registered wasm reservations to Go's original handler.
	MOV	$7, T4                       // Linux SIGBUS
	BEQ	A0, T4, chainBus
	MOV	·guardOldSEGVHandler(SB), T3
	JALR	ZERO, T3
chainBus:
	MOV	·guardOldBUSHandler(SB), T3
	JALR	ZERO, T3

// nativeTrapExitHandlerJump runs after the kernel restores the rewritten signal
// context. S9 still holds linMem; basedata records the foreign-stack base and the
// enterNative/resumeNative continuation.
TEXT ·nativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-0
	MOV	-24(X25), T0
	MOV	T0, SP
	MOV	-32(X25), T0
	JALR	ZERO, T0

TEXT ·addrGuardSigHandler(SB), NOSPLIT, $0-8
	MOV	$·guardSigHandler(SB), A0
	MOV	A0, ret+0(FP)
	RET

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT, $0-8
	MOV	$·nativeTrapExitHandlerJump(SB), A0
	MOV	A0, ret+0(FP)
	RET
