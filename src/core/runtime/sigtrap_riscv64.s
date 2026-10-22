//go:build linux && riscv64 && wago_guardpage && !tinygo && !wago_target_tinygo

#include "textflag.h"
#include "go_asm.h"

// Linux RV64: si_addr@16, ucontext.mcontext@176, saved PC@176,
// saved X25/s9@376. Registry rows have start/end/base/owner at 0/8/16/24.
// The handler never calls Go or writes gp/tp. Original signal arguments remain
// in A0/A1/A2 until a matching Wasm reservation is authenticated.
TEXT ·guardSigHandler(SB), NOSPLIT|NOFRAME, $0-0
 MOV A2, T0
 MOV 16(A1), T1
 MOV $·guardRegions(SB), T2
 MOV $256, T3
scan:
 MOV (T2), T4
 FENCE
 BEQ T4, ZERO, next
 BLTU T1, T4, next
 MOV 8(T2), T4
 BGEU T1, T4, next
 MOV 16(T2), T4
 MOV 24(T2), T5
 FENCE
 MOV const_guardRV64HomeOffset(T0), T6
 BNE T5, T6, next
 SUB T4, T1, T1
 MOV -288(T4), T5
 BGEU T1, T5, dotrap
 // A0 = base + (reservation-relative offset rounded down to64KiB).
 MOV $-65536, T5
 AND T5, T1, A0
 ADD T4, A0, A0
 MOV $65536, A1
 MOV $3, A2
 MOV $226, A7
 ECALL
 BLT A0, ZERO, commitfailed
 RET
commitfailed:
 MOV $4, T5
 JMP settrap
dotrap:
 MOV $3, T5
settrap:
 MOV const_guardRV64HomeOffset(T0), T6
 MOV -104(T6), T4
 MOVW T5, (T4)
 // Preserve source ownership for the cold trap decorator. The signal PC is
 // not a Wasm source offset; enterNative cleared that field before execution.
 MOV -const_guardRV64NativeContextOffset(T6), T5
 MOV T5, 8(T4)
 MOV T6, const_guardRV64LandingHomeOffset(T0) // saved X5/t0 carries the authenticated home to landing pad
 MOV ·guardTrapExitHandlerJumpPC(SB), T4
 MOV T4, const_guardRV64PCOffset(T0)
 RET
next:
 ADD $32, T2, T2
 ADD $-1, T3, T3
 BNE T3, ZERO, scan
 MOV $7, T4
 BEQ A0, T4, chainbus
 MOV ·guardOldSEGVHandler(SB), T4
 JMP (T4)
chainbus:
 MOV ·guardOldBUSHandler(SB), T4
 JMP (T4)

TEXT ·nativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-0
 MOV -const_RV64TrapReentrySPOffset(T0), X2
 MOV -const_RV64TrapContinuationOffset(T0), T1
 JMP (T1)

TEXT ·addrGuardSigHandler(SB), NOSPLIT|NOFRAME, $0-8
 MOV $·guardSigHandler(SB), A0
 MOV A0, ret+0(FP)
 RET

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-8
 MOV $·nativeTrapExitHandlerJump(SB), A0
 MOV A0, ret+0(FP)
 RET
