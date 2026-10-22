//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

#include "textflag.h"
#include "go_asm.h"

// Internal park/resume proof. Check the complete new bridge and parked frame
// before any foreign-stack write. Save current Go thread state on every resume.
TEXT ·resumeNativeRV64Raw(SB),NOSPLIT|NOFRAME,$0-24
 MOV ctrl+0(FP), T0
 MOV trap+8(FP), A2
 MOV stack+16(FP), T1
 BEQ A2, ZERO, rejected
 BEQ T0, ZERO, bounds
 AND $7, T0, T4
 BNE T4, ZERO, bounds
 AND $15, T1, T4
 BNE T4, ZERO, bounds
 MOV rv64ParkFrame_S9(T0), A1
 BEQ A1, ZERO, bounds
 MOV -72(A1), T2
 BEQ T2, ZERO, bounds
 BLTU T1, T2, bounds
 SUB T2, T1, T4
 MOV $(const_nativeWrapperSaveBytes+16), T5
 BLTU T4, T5, bounds
 ADD $-const_nativeWrapperSaveBytes, T1, T1
 MOV rv64ParkFrame_BridgeSP(T0), T4
 BNE T1, T4, bounds
 MOV rv64ParkFrame_SP(T0), T3
 AND $15, T3, T4
 BNE T4, ZERO, bounds
 BLTU T3, T2, bounds
 ADD $-16, T1, T4
 BLTU T4, T3, bounds
 MOV rv64ParkFrame_RA(T0), T4
 BEQ T4, ZERO, bounds
 AND $3, T4, T5
 BNE T5, ZERO, bounds
 MOV X2, nativeWrapperSaveFrame_GoSP(T1)
 MOV RA, nativeWrapperSaveFrame_RA(T1)
 MOV GP, nativeWrapperSaveFrame_GP(T1)
 MOV TP, nativeWrapperSaveFrame_TP(T1)
 MOV X8, nativeWrapperSaveFrame_S0(T1)
 MOV X9, nativeWrapperSaveFrame_S1(T1)
 MOV X18, nativeWrapperSaveFrame_S2(T1)
 MOV X19, nativeWrapperSaveFrame_S3(T1)
 MOV X20, nativeWrapperSaveFrame_S4(T1)
 MOV X21, nativeWrapperSaveFrame_S5(T1)
 MOV X22, nativeWrapperSaveFrame_S6(T1)
 MOV X23, nativeWrapperSaveFrame_S7(T1)
 MOV X24, nativeWrapperSaveFrame_S8(T1)
 MOV X25, nativeWrapperSaveFrame_S9(T1)
 MOV CTXT, nativeWrapperSaveFrame_CTXT(T1)
 MOV g, nativeWrapperSaveFrame_G(T1)
 // CSRRS X31,fcsr,X0. Preserve accrued flags and the caller's rounding mode.
 WORD $0x00302ff3
 MOV T6, nativeWrapperSaveFrame_FCSR(T1)
 MOV A1, X25
 MOV T1, X2
 MOV A2, -104(A1)
 MOV T1, -const_RV64TrapReentrySPOffset(A1)
 JAL RA, resumeGuest

afterResume:
 MOV nativeWrapperSaveFrame_FCSR(X2), T6
 // CSRRW X0,fcsr,X31. Normal and cold-trap returns share this restoration.
 WORD $0x003f9073
 MOV nativeWrapperSaveFrame_RA(X2), RA
 MOV nativeWrapperSaveFrame_GP(X2), GP
 MOV nativeWrapperSaveFrame_TP(X2), TP
 MOV nativeWrapperSaveFrame_S0(X2), X8
 MOV nativeWrapperSaveFrame_S1(X2), X9
 MOV nativeWrapperSaveFrame_S2(X2), X18
 MOV nativeWrapperSaveFrame_S3(X2), X19
 MOV nativeWrapperSaveFrame_S4(X2), X20
 MOV nativeWrapperSaveFrame_S5(X2), X21
 MOV nativeWrapperSaveFrame_S6(X2), X22
 MOV nativeWrapperSaveFrame_S7(X2), X23
 MOV nativeWrapperSaveFrame_S8(X2), X24
 MOV nativeWrapperSaveFrame_S9(X2), X25
 MOV nativeWrapperSaveFrame_CTXT(X2), CTXT
 MOV nativeWrapperSaveFrame_G(X2), g
 MOV nativeWrapperSaveFrame_GoSP(X2), T1
 MOV T1, X2
 RET

resumeGuest:
 MOV RA, -const_RV64TrapContinuationOffset(A1)
 MOV rv64ParkFrame_FCSR(T0), T6
 WORD $0x003f9073
 MOV rv64ParkFrame_S0(T0), X8
 MOV rv64ParkFrame_S1(T0), X9
 MOV rv64ParkFrame_S2(T0), X18
 MOV rv64ParkFrame_S3(T0), X19
 MOV rv64ParkFrame_S4(T0), X20
 MOV rv64ParkFrame_S5(T0), X21
 MOV rv64ParkFrame_S6(T0), X22
 MOV rv64ParkFrame_S7(T0), X23
 MOV rv64ParkFrame_S8(T0), X24
 MOV rv64ParkFrame_S9(T0), X25
 MOVD rv64ParkFrame_F8(T0), F8
 MOVD rv64ParkFrame_F9(T0), F9
 MOVD rv64ParkFrame_F18(T0), F18
 MOVD rv64ParkFrame_F19(T0), F19
 MOVD rv64ParkFrame_F20(T0), F20
 MOVD rv64ParkFrame_F21(T0), F21
 MOVD rv64ParkFrame_F22(T0), F22
 MOVD rv64ParkFrame_F23(T0), F23
 MOVD rv64ParkFrame_F24(T0), F24
 MOVD rv64ParkFrame_F25(T0), F25
 MOVD rv64ParkFrame_F26(T0), F26
 MOVD rv64ParkFrame_F27(T0), F27
 MOV rv64ParkFrame_RA(T0), RA
 MOV rv64ParkFrame_SP(T0), X2
 JMP (RA)

bounds:
 MOV $const_TrapStackFenceBreached, T0
 MOVW T0, (A2)
rejected:
 RET
