//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

#include "textflag.h"
#include "go_asm.h"

// Production ABI0 bridge. Keep gp/tp/g valid throughout native execution.
// The complete foreign save area and minimum guest header are checked before
// writes. Each compiled guest frame must independently check its full descent.
TEXT ·enterNativeIntRV64Raw(SB),NOSPLIT|NOFRAME,$0-72
 MOV code+0(FP), T0
 MOV linMem+8(FP), A1
 MOV trap+16(FP), A2
 // Source payload belongs to this entry, including bridge-only rejection.
 // Preserve the atomic interruption code; never reuse an earlier origin/PC.
 MOV ZERO, 8(A2)
 MOV ZERO, 16(A2)
 MOV foreignStackTop+32(FP), T1
 BEQ T0, ZERO, bounds
 BEQ A1, ZERO, bounds
 AND $15, T1, T4
 BNE T4, ZERO, bounds
 MOV -72(A1), T2
 BEQ T2, ZERO, bounds
 BLTU T1, T2, bounds
 SUB T2, T1, T4
 MOV $(const_nativeWrapperSaveBytes+16), T5
 BLTU T4, T5, bounds

 ADD $-const_nativeWrapperSaveBytes, T1, T1
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
 MOV ZERO, nativeWrapperSaveFrame_Padding(T1) // invocation-owned EH handler chain
 MOV A1, X25
 MOV a0+40(FP), A0
 MOV a1+48(FP), A1
 MOV a2+56(FP), A2
 MOV a3+64(FP), A3
 MOV T1, X2
 MOV ZERO, X8
 MOV T1, -const_RV64TrapReentrySPOffset(X25)
 JAL RA, callNative

afterNativeCall:
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

callNative:
 MOV RA, -const_RV64TrapContinuationOffset(X25)
 JALR RA, T0
 JMP afterNativeCall

bounds:
 MOV $const_TrapStackFenceBreached, T0
 MOVW T0, (A2)
 RET
