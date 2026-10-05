//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

#include "textflag.h"

// Test-only ABI0 probe. No Go allocation/callback occurs during the parked syscall.
TEXT ·probeNativeWrapperRaw(SB),NOSPLIT,$240-56
	MOV X8, 80(X2)
	MOV X9, 88(X2)
	MOV X18, 96(X2)
	MOV X19, 104(X2)
	MOV X20, 112(X2)
	MOV X21, 120(X2)
	MOV X22, 128(X2)
	MOV X23, 136(X2)
	MOV X24, 144(X2)
	MOV X25, 152(X2)
	MOV CTXT, 160(X2)
	MOV GP, 168(X2)
	MOV TP, 176(X2)
	MOV g, 184(X2)
	MOV X2, 192(X2)
	MOV code+0(FP), T0
	MOV T0, 8(X2)
	MOV trace+8(FP), T0
	MOV T0, 16(X2)
	MOV linMem+16(FP), T0
	MOV T0, 24(X2)
	MOV trap+24(FP), T0
	MOV T0, 32(X2)
	MOV results+32(FP), T0
	MOV T0, 40(X2)
	MOV top+40(FP), T0
	MOV T0, 48(X2)
	MOV $264, X8
	MOV $265, X9
	MOV $274, X18
	MOV $275, X19
	MOV $276, X20
	MOV $277, X21
	MOV $278, X22
	MOV $279, X23
	MOV $280, X24
	MOV $281, X25
	MOV $282, CTXT
	CALL ·enterNativeRaw(SB)
	MOV ZERO, T0
	MOV $264, T1
	BEQ X8, T1, probe_check_0
	OR $1, T0, T0
probe_check_0:
	MOV $265, T1
	BEQ X9, T1, probe_check_1
	OR $2, T0, T0
probe_check_1:
	MOV $274, T1
	BEQ X18, T1, probe_check_2
	OR $4, T0, T0
probe_check_2:
	MOV $275, T1
	BEQ X19, T1, probe_check_3
	OR $8, T0, T0
probe_check_3:
	MOV $276, T1
	BEQ X20, T1, probe_check_4
	OR $16, T0, T0
probe_check_4:
	MOV $277, T1
	BEQ X21, T1, probe_check_5
	OR $32, T0, T0
probe_check_5:
	MOV $278, T1
	BEQ X22, T1, probe_check_6
	OR $64, T0, T0
probe_check_6:
	MOV $279, T1
	BEQ X23, T1, probe_check_7
	OR $128, T0, T0
probe_check_7:
	MOV $280, T1
	BEQ X24, T1, probe_check_8
	OR $256, T0, T0
probe_check_8:
	MOV $281, T1
	BEQ X25, T1, probe_check_9
	OR $512, T0, T0
probe_check_9:
	MOV $282, T1
	BEQ CTXT, T1, probe_check_10
	OR $1024, T0, T0
probe_check_10:
	MOV 168(X2), T1
	BEQ GP, T1, probe_check_11
	OR $2048, T0, T0
probe_check_11:
	MOV 176(X2), T1
	BEQ TP, T1, probe_check_12
	OR $4096, T0, T0
probe_check_12:
	MOV 184(X2), T1
	BEQ g, T1, probe_check_13
	OR $8192, T0, T0
probe_check_13:
	MOV 192(X2), T1
	BEQ X2, T1, probe_check_14
	OR $16384, T0, T0
probe_check_14:
	MOV trace+8(FP), T1
	MOV 0(T1), T2
	BEQ GP, T2, probe_live_0
	OR $131072, T0, T0
probe_live_0:
	MOV 8(T1), T2
	BEQ TP, T2, probe_live_1
	OR $262144, T0, T0
probe_live_1:
	MOV 16(T1), T2
	BEQ g, T2, probe_live_2
	OR $524288, T0, T0
probe_live_2:
	MOV 24(T1), T2
	AND $15, T2, T2
	BEQ T2, ZERO, probe_aligned
	OR $1048576, T0, T0
probe_aligned:
	MOV 80(X2), X8
	MOV 88(X2), X9
	MOV 96(X2), X18
	MOV 104(X2), X19
	MOV 112(X2), X20
	MOV 120(X2), X21
	MOV 128(X2), X22
	MOV 136(X2), X23
	MOV 144(X2), X24
	MOV 152(X2), X25
	MOV 160(X2), CTXT
	MOV T0, ret+48(FP)
	RET
