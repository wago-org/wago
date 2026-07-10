//go:build linux && arm64 && wago_guardpage

#include "textflag.h"

// Linux arm64 ucontext offsets used below:
//   siginfo.si_addr = +16
//   ucontext.uc_mcontext = +176
//   sigcontext.regs[26] = uc_mcontext + 8 + 26*8 = +392
//   sigcontext.pc = uc_mcontext + 8 + 31*8 + 8 = +440
// guardRegion is {start@0, end@8, linMem@16}, 32 bytes.
TEXT ·guardSigHandler(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	16(R1), R8              // R8 = siginfo->si_addr (fault address)
	MOVD	$·guardRegions(SB), R10 // R10 = &guardRegions[0]
	MOVD	$256, R11               // R11 = slots left (maxGuardRegions)
scan:
	MOVD	0(R10), R9              // region.start
	CBZ	R9, next                // free slot
	CMP	R9, R8
	BLO	next                    // addr < start
	MOVD	8(R10), R9              // region.end
	CMP	R9, R8
	BHS	next                    // addr >= end

	MOVD	16(R10), R9             // region.linMem
	MOVD	392(R2), R26            // saved X26 (arm64 linMem)
	CMP	R9, R26
	BNE	next                    // mismatch -> not this reservation's wasm fault

	// off = fault - linMem; curBytes = [linMem-8].
	MOVD	R8, R12
	SUB	R26, R12                // R12 = off (fault - linMem)
	MOVWU	-8(R26), R13            // R13 = curBytes (u32, zero-extended)
	CMP	R13, R12
	BHS	dotrap                  // curBytes <= off -> out of range -> trap

	// Commit the 64 KiB wasm page containing the fault, then resume the access.
	MOVD	R8, R0
	AND	$-65536, R0             // align down to wasm page
	MOVD	$65536, R1
	MOVD	$3, R2                  // PROT_READ|PROT_WRITE
	MOVD	$226, R8                // SYS_mprotect
	SVC
	RET                             // -> kernel signal return: retry access

dotrap:
	MOVD	-104(R26), R12          // R12 = trap cell pointer
	MOVW	$3, R13                 // TrapLinMemOutOfBounds
	MOVW	R13, (R12)
	MOVD	·guardTrapExitHandlerJumpPC(SB), R9
	MOVD	R9, 440(R2)             // saved PC = nativeTrapExitHandlerJump
	RET                             // -> kernel signal return -> nativeTrapExitHandlerJump

next:
	ADD	$32, R10
	SUB	$1, R11
	CBNZ	R11, scan
	MOVD	·guardOldHandler(SB), R9
	B	(R9)

// nativeTrapExitHandlerJump is the arm64/WARP landing pad. X26 is still the
// faulting frame's linMem after sigreturn; [X26-24] is the trampoline-recorded
// re-entry SP and [X26-32] is the trampoline continuation PC. Unlike amd64, arm64
// BL does not push a return address, so branch to the saved continuation directly.
TEXT ·nativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	-24(R26), R9
	MOVD	R9, RSP
	MOVD	-32(R26), R9
	B	(R9)

TEXT ·addrGuardSigHandler(SB), NOSPLIT, $0-8
	MOVD	$·guardSigHandler(SB), R0
	MOVD	R0, ret+0(FP)
	RET

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT, $0-8
	MOVD	$·nativeTrapExitHandlerJump(SB), R0
	MOVD	R0, ret+0(FP)
	RET
