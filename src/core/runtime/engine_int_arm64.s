// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64 && !tinygo

#include "textflag.h"
#include "standalone_registers.h"

// func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
TEXT ·enterNativeIntRaw(SB), NOSPLIT, $0-64
	MOVD code+0(FP), R9
	MOVD foreignStackTop+48(FP), R10
	SUB  $112, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	STP  (R19, R20), 8(R10)
	STP  (R21, R22), 24(R10)
	STP  (R23, R24), 40(R10)
	STP  (R25, R26), 56(R10)
	STP  (R27, g), 72(R10)
	STP  (R29, R30), 88(R10)

	MOVD linMem+8(FP), R26
#ifdef WAGO_INT_FIXED_ARG_0
	MOVD a0+16(FP), R0
#endif
#ifdef WAGO_INT_FIXED_ARG_1
	MOVD a1+24(FP), R1
#endif
#ifdef WAGO_INT_FIXED_ARG_2
	MOVD a2+32(FP), R2
#endif
#ifdef WAGO_INT_FIXED_ARG_3
	MOVD a3+40(FP), R3
#endif
	MOVD R10, RSP
	MOVD ZR, R22
	MOVD ZR, R29
	MOVD R10, -24(R26)
	ADR  afterNativeIntCall, R11
	MOVD R11, -32(R26)
	BL   (R9)

afterNativeIntCall:
	LDP  8(RSP), (R19, R20)
	LDP  24(RSP), (R21, R22)
	LDP  40(RSP), (R23, R24)
	LDP  56(RSP), (R25, R26)
	LDP  72(RSP), (R27, g)
	LDP  88(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
#ifdef WAGO_INT_FIXED_RESULT
	MOVD R0, ret+56(FP)
#else
	MOVD ZR, ret+56(FP)
#endif
	RET

// func enterNativeIntPairRaw(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) (uintptr, uintptr)
// The internal integer ABI returns two values in X0/X1.
TEXT ·enterNativeIntPairRaw(SB), NOSPLIT, $0-72
	MOVD code+0(FP), R9
	MOVD foreignStackTop+48(FP), R10
	SUB  $112, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	STP  (R19, R20), 8(R10)
	STP  (R21, R22), 24(R10)
	STP  (R23, R24), 40(R10)
	STP  (R25, R26), 56(R10)
	STP  (R27, g), 72(R10)
	STP  (R29, R30), 88(R10)

	MOVD linMem+8(FP), R26
#ifdef WAGO_INT_PAIR_ARG_0
	MOVD a0+16(FP), R0
#endif
#ifdef WAGO_INT_PAIR_ARG_1
	MOVD a1+24(FP), R1
#endif
#ifdef WAGO_INT_PAIR_ARG_2
	MOVD a2+32(FP), R2
#endif
#ifdef WAGO_INT_PAIR_ARG_3
	MOVD a3+40(FP), R3
#endif
	MOVD R10, RSP
	MOVD ZR, R22
	MOVD ZR, R29
	MOVD R10, -24(R26)
	ADR  afterNativeIntPairCall, R11
	MOVD R11, -32(R26)
	BL   (R9)

afterNativeIntPairCall:
	LDP  8(RSP), (R19, R20)
	LDP  24(RSP), (R21, R22)
	LDP  40(RSP), (R23, R24)
	LDP  56(RSP), (R25, R26)
	LDP  72(RSP), (R27, g)
	LDP  88(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
	MOVD R0, ret+56(FP)
	MOVD R1, ret1+64(FP)
	RET

// func enterNativeIntWideRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr, uintptr)
// The compiler's integer register ABI admits eight parameters on arm64.
TEXT ·enterNativeIntWideRaw(SB), NOSPLIT, $0-72
	MOVD code+0(FP), R9
	MOVD foreignStackTop+24(FP), R10
#ifdef WAGO_INT_WIDE_ARGS
	MOVD args+16(FP), R11
#endif
	SUB  $112, R10, R10
	MOVD RSP, R12
	MOVD R12, 0(R10)
	STP  (R19, R20), 8(R10)
	STP  (R21, R22), 24(R10)
	STP  (R23, R24), 40(R10)
	STP  (R25, R26), 56(R10)
	STP  (R27, g), 72(R10)
	STP  (R29, R30), 88(R10)

	MOVD linMem+8(FP), R26
	MOVD R10, RSP
#ifdef WAGO_INT_WIDE_ARG_0
	MOVD  0(R11), R0
#endif
#ifdef WAGO_INT_WIDE_ARG_1
	MOVD  8(R11), R1
#endif
#ifdef WAGO_INT_WIDE_ARG_2
	MOVD 16(R11), R2
#endif
#ifdef WAGO_INT_WIDE_ARG_3
	MOVD 24(R11), R3
#endif
#ifdef WAGO_INT_WIDE_ARG_4
	MOVD 32(R11), R4
#endif
#ifdef WAGO_INT_WIDE_ARG_5
	MOVD 40(R11), R5
#endif
#ifdef WAGO_INT_WIDE_ARG_6
	MOVD 48(R11), R6
#endif
#ifdef WAGO_INT_WIDE_ARG_7
	MOVD 56(R11), R7
#endif
	MOVD ZR, R22
	MOVD ZR, R29
	MOVD R10, -24(R26)
	ADR  afterNativeIntWideCall, R12
	MOVD R12, -32(R26)
	BL   (R9)

afterNativeIntWideCall:
	LDP  8(RSP), (R19, R20)
	LDP  24(RSP), (R21, R22)
	LDP  40(RSP), (R23, R24)
	LDP  56(RSP), (R25, R26)
	LDP  72(RSP), (R27, g)
	LDP  88(RSP), (R29, R30)
	MOVD 0(RSP), R12
	MOVD R12, RSP
#ifdef WAGO_INT_WIDE_RESULT_0
	MOVD R0, ret+32(FP)
#else
	MOVD ZR, ret+32(FP)
#endif
#ifdef WAGO_INT_WIDE_RESULT_1
	MOVD R1, ret1+40(FP)
#else
	MOVD ZR, ret1+40(FP)
#endif
#ifdef WAGO_INT_WIDE_RESULT_2
	MOVD R2, ret2+48(FP)
#else
	MOVD ZR, ret2+48(FP)
#endif
#ifdef WAGO_INT_WIDE_RESULT_3
	MOVD R3, ret3+56(FP)
#else
	MOVD ZR, ret3+56(FP)
#endif
#ifdef WAGO_INT_WIDE_RESULT_4
	MOVD R4, ret4+64(FP)
#else
	MOVD ZR, ret4+64(FP)
#endif
	RET

// func enterNativeIntOctRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr)
TEXT ·enterNativeIntOctRaw(SB), NOSPLIT, $0-96
	MOVD code+0(FP), R9
	MOVD foreignStackTop+24(FP), R10
#ifdef WAGO_INT_OCT_ARGS
	MOVD args+16(FP), R11
#endif
	SUB  $112, R10, R10
	MOVD RSP, R12
	MOVD R12, 0(R10)
	STP  (R19, R20), 8(R10)
	STP  (R21, R22), 24(R10)
	STP  (R23, R24), 40(R10)
	STP  (R25, R26), 56(R10)
	STP  (R27, g), 72(R10)
	STP  (R29, R30), 88(R10)

	MOVD linMem+8(FP), R26
	MOVD R10, RSP
#ifdef WAGO_INT_OCT_ARG_0
	MOVD  0(R11), R0
#endif
#ifdef WAGO_INT_OCT_ARG_1
	MOVD  8(R11), R1
#endif
#ifdef WAGO_INT_OCT_ARG_2
	MOVD 16(R11), R2
#endif
#ifdef WAGO_INT_OCT_ARG_3
	MOVD 24(R11), R3
#endif
#ifdef WAGO_INT_OCT_ARG_4
	MOVD 32(R11), R4
#endif
#ifdef WAGO_INT_OCT_ARG_5
	MOVD 40(R11), R5
#endif
#ifdef WAGO_INT_OCT_ARG_6
	MOVD 48(R11), R6
#endif
#ifdef WAGO_INT_OCT_ARG_7
	MOVD 56(R11), R7
#endif
	MOVD ZR, R22
	MOVD ZR, R29
	MOVD R10, -24(R26)
	ADR  afterNativeIntOctCall, R12
	MOVD R12, -32(R26)
	BL   (R9)

afterNativeIntOctCall:
	LDP  8(RSP), (R19, R20)
	LDP  24(RSP), (R21, R22)
	LDP  40(RSP), (R23, R24)
	LDP  56(RSP), (R25, R26)
	LDP  72(RSP), (R27, g)
	LDP  88(RSP), (R29, R30)
	MOVD 0(RSP), R12
	MOVD R12, RSP
#ifdef WAGO_INT_OCT_RESULT_0
	MOVD R0, ret+32(FP)
#else
	MOVD ZR, ret+32(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_1
	MOVD R1, ret1+40(FP)
#else
	MOVD ZR, ret1+40(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_2
	MOVD R2, ret2+48(FP)
#else
	MOVD ZR, ret2+48(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_3
	MOVD R3, ret3+56(FP)
#else
	MOVD ZR, ret3+56(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_4
	MOVD R4, ret4+64(FP)
#else
	MOVD ZR, ret4+64(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_5
	MOVD R5, ret5+72(FP)
#else
	MOVD ZR, ret5+72(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_6
	MOVD R6, ret6+80(FP)
#else
	MOVD ZR, ret6+80(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_7
	MOVD R7, ret7+88(FP)
#else
	MOVD ZR, ret7+88(FP)
#endif
	RET

// func enterNativeFloatRaw(code, linMem uintptr, args *[4]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr)
// Float-only parameters use V0..V3; results return in V0..V3.
TEXT ·enterNativeFloatRaw(SB), NOSPLIT, $0-64
	MOVD code+0(FP), R9
	MOVD foreignStackTop+24(FP), R10
#ifdef WAGO_FLOAT_FOUR_ARGS
	MOVD args+16(FP), R11
#endif
	SUB  $112, R10, R10
	MOVD RSP, R12
	MOVD R12, 0(R10)
	STP  (R19, R20), 8(R10)
	STP  (R21, R22), 24(R10)
	STP  (R23, R24), 40(R10)
	STP  (R25, R26), 56(R10)
	STP  (R27, g), 72(R10)
	STP  (R29, R30), 88(R10)

	MOVD linMem+8(FP), R26
	MOVD R10, RSP
#ifdef WAGO_FLOAT_FOUR_ARG_0
	MOVD  0(R11), R0
#endif
#ifdef WAGO_FLOAT_FOUR_ARG_1
	MOVD  8(R11), R1
#endif
#ifdef WAGO_FLOAT_FOUR_ARG_2
	MOVD 16(R11), R2
#endif
#ifdef WAGO_FLOAT_FOUR_ARG_3
	MOVD 24(R11), R3
#endif
#ifdef WAGO_FLOAT_FOUR_ARG_0
	FMOVD R0, F0
#endif
#ifdef WAGO_FLOAT_FOUR_ARG_1
	FMOVD R1, F1
#endif
#ifdef WAGO_FLOAT_FOUR_ARG_2
	FMOVD R2, F2
#endif
#ifdef WAGO_FLOAT_FOUR_ARG_3
	FMOVD R3, F3
#endif
	MOVD ZR, R22
	MOVD ZR, R29
	MOVD R10, -24(R26)
	ADR  afterNativeFloatCall, R12
	MOVD R12, -32(R26)
	BL   (R9)

afterNativeFloatCall:
#ifdef WAGO_FLOAT_FOUR_RESULT_0
	FMOVD F0, R0
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_1
	FMOVD F1, R1
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_2
	FMOVD F2, R2
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_3
	FMOVD F3, R3
#endif
	LDP  8(RSP), (R19, R20)
	LDP  24(RSP), (R21, R22)
	LDP  40(RSP), (R23, R24)
	LDP  56(RSP), (R25, R26)
	LDP  72(RSP), (R27, g)
	LDP  88(RSP), (R29, R30)
	MOVD 0(RSP), R12
	MOVD R12, RSP
#ifdef WAGO_FLOAT_FOUR_RESULT_0
	MOVD R0, ret+32(FP)
#else
	MOVD ZR, ret+32(FP)
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_1
	MOVD R1, ret1+40(FP)
#else
	MOVD ZR, ret1+40(FP)
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_2
	MOVD R2, ret2+48(FP)
#else
	MOVD ZR, ret2+48(FP)
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_3
	MOVD R3, ret3+56(FP)
#else
	MOVD ZR, ret3+56(FP)
#endif
	RET

// func enterNativeFloatOctRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr)
TEXT ·enterNativeFloatOctRaw(SB), NOSPLIT, $0-96
	MOVD code+0(FP), R9
	MOVD foreignStackTop+24(FP), R10
#ifdef WAGO_FLOAT_OCT_ARGS
	MOVD args+16(FP), R11
#endif
	SUB  $112, R10, R10
	MOVD RSP, R12
	MOVD R12, 0(R10)
	STP  (R19, R20), 8(R10)
	STP  (R21, R22), 24(R10)
	STP  (R23, R24), 40(R10)
	STP  (R25, R26), 56(R10)
	STP  (R27, g), 72(R10)
	STP  (R29, R30), 88(R10)
	MOVD linMem+8(FP), R26
	MOVD R10, RSP
#ifdef WAGO_FLOAT_OCT_ARG_0
	MOVD  0(R11), R0
#endif
#ifdef WAGO_FLOAT_OCT_ARG_1
	MOVD  8(R11), R1
#endif
#ifdef WAGO_FLOAT_OCT_ARG_2
	MOVD 16(R11), R2
#endif
#ifdef WAGO_FLOAT_OCT_ARG_3
	MOVD 24(R11), R3
#endif
#ifdef WAGO_FLOAT_OCT_ARG_4
	MOVD 32(R11), R4
#endif
#ifdef WAGO_FLOAT_OCT_ARG_5
	MOVD 40(R11), R5
#endif
#ifdef WAGO_FLOAT_OCT_ARG_6
	MOVD 48(R11), R6
#endif
#ifdef WAGO_FLOAT_OCT_ARG_7
	MOVD 56(R11), R7
#endif
#ifdef WAGO_FLOAT_OCT_ARG_0
	FMOVD R0, F0
#endif
#ifdef WAGO_FLOAT_OCT_ARG_1
	FMOVD R1, F1
#endif
#ifdef WAGO_FLOAT_OCT_ARG_2
	FMOVD R2, F2
#endif
#ifdef WAGO_FLOAT_OCT_ARG_3
	FMOVD R3, F3
#endif
#ifdef WAGO_FLOAT_OCT_ARG_4
	FMOVD R4, F4
#endif
#ifdef WAGO_FLOAT_OCT_ARG_5
	FMOVD R5, F5
#endif
#ifdef WAGO_FLOAT_OCT_ARG_6
	FMOVD R6, F6
#endif
#ifdef WAGO_FLOAT_OCT_ARG_7
	FMOVD R7, F7
#endif
	MOVD ZR, R22
	MOVD ZR, R29
	MOVD R10, -24(R26)
	ADR  afterNativeFloatOctCall, R12
	MOVD R12, -32(R26)
	BL   (R9)

afterNativeFloatOctCall:
#ifdef WAGO_FLOAT_OCT_RESULT_0
	FMOVD F0, R0
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_1
	FMOVD F1, R1
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_2
	FMOVD F2, R2
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_3
	FMOVD F3, R3
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_4
	FMOVD F4, R4
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_5
	FMOVD F5, R5
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_6
	FMOVD F6, R6
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_7
	FMOVD F7, R7
#endif
	LDP  8(RSP), (R19, R20)
	LDP  24(RSP), (R21, R22)
	LDP  40(RSP), (R23, R24)
	LDP  56(RSP), (R25, R26)
	LDP  72(RSP), (R27, g)
	LDP  88(RSP), (R29, R30)
	MOVD 0(RSP), R12
	MOVD R12, RSP
#ifdef WAGO_FLOAT_OCT_RESULT_0
	MOVD R0, ret+32(FP)
#else
	MOVD ZR, ret+32(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_1
	MOVD R1, ret1+40(FP)
#else
	MOVD ZR, ret1+40(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_2
	MOVD R2, ret2+48(FP)
#else
	MOVD ZR, ret2+48(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_3
	MOVD R3, ret3+56(FP)
#else
	MOVD ZR, ret3+56(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_4
	MOVD R4, ret4+64(FP)
#else
	MOVD ZR, ret4+64(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_5
	MOVD R5, ret5+72(FP)
#else
	MOVD ZR, ret5+72(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_6
	MOVD R6, ret6+80(FP)
#else
	MOVD ZR, ret6+80(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_7
	MOVD R7, ret7+88(FP)
#else
	MOVD ZR, ret7+88(FP)
#endif
	RET

// func enterNativeMixedRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr)
// GP args occupy X0..X3; FP args independently occupy V0..V3.
TEXT ·enterNativeMixedRaw(SB), NOSPLIT, $0-64
	MOVD code+0(FP), R9
	MOVD foreignStackTop+24(FP), R10
#ifdef WAGO_MIXED_ARGS
	MOVD args+16(FP), R11
#endif
	SUB  $112, R10, R10
	MOVD RSP, R12
	MOVD R12, 0(R10)
	STP  (R19, R20), 8(R10)
	STP  (R21, R22), 24(R10)
	STP  (R23, R24), 40(R10)
	STP  (R25, R26), 56(R10)
	STP  (R27, g), 72(R10)
	STP  (R29, R30), 88(R10)

	MOVD linMem+8(FP), R26
	MOVD R10, RSP
#ifdef WAGO_MIXED_GP_ARG_0
	MOVD  0(R11), R0
#endif
#ifdef WAGO_MIXED_GP_ARG_1
	MOVD  8(R11), R1
#endif
#ifdef WAGO_MIXED_GP_ARG_2
	MOVD 16(R11), R2
#endif
#ifdef WAGO_MIXED_GP_ARG_3
	MOVD 24(R11), R3
#endif
#ifdef WAGO_MIXED_FP_ARG_0
	MOVD 32(R11), R4
	FMOVD R4, F0
#endif
#ifdef WAGO_MIXED_FP_ARG_1
	MOVD 40(R11), R4
	FMOVD R4, F1
#endif
#ifdef WAGO_MIXED_FP_ARG_2
	MOVD 48(R11), R4
	FMOVD R4, F2
#endif
#ifdef WAGO_MIXED_FP_ARG_3
	MOVD 56(R11), R4
	FMOVD R4, F3
#endif
	MOVD ZR, R22
	MOVD ZR, R29
	MOVD R10, -24(R26)
	ADR  afterNativeMixedCall, R12
	MOVD R12, -32(R26)
	BL   (R9)

afterNativeMixedCall:
#ifdef WAGO_MIXED_FP_RESULT_0
	FMOVD F0, R4
#endif
#ifdef WAGO_MIXED_FP_RESULT_1
	FMOVD F1, R5
#endif
	LDP  8(RSP), (R19, R20)
	LDP  24(RSP), (R21, R22)
	LDP  40(RSP), (R23, R24)
	LDP  56(RSP), (R25, R26)
	LDP  72(RSP), (R27, g)
	LDP  88(RSP), (R29, R30)
	MOVD 0(RSP), R12
	MOVD R12, RSP
#ifdef WAGO_MIXED_GP_RESULT_0
	MOVD R0, ret+32(FP)
#else
	MOVD ZR, ret+32(FP)
#endif
#ifdef WAGO_MIXED_GP_RESULT_1
	MOVD R1, ret1+40(FP)
#else
	MOVD ZR, ret1+40(FP)
#endif
#ifdef WAGO_MIXED_FP_RESULT_0
	MOVD R4, ret2+48(FP)
#else
	MOVD ZR, ret2+48(FP)
#endif
#ifdef WAGO_MIXED_FP_RESULT_1
	MOVD R5, ret3+56(FP)
#else
	MOVD ZR, ret3+56(FP)
#endif
	RET

// func enterNativeIntLightRaw(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
// Compiler proof: generated code touches no callee-saved register except X26,
// which this thunk establishes as the linear-memory base. Preserve the Go stack,
// X26, FP, and LR; entersyscall/exitsyscall remain in the Go wrapper.
TEXT ·enterNativeIntLightRaw(SB), NOSPLIT, $0-64
	MOVD code+0(FP), R9
	MOVD foreignStackTop+48(FP), R10
	SUB  $32, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	MOVD R26, 8(R10)
	STP  (R29, R30), 16(R10)

	MOVD linMem+8(FP), R26
#ifdef WAGO_INT_FIXED_ARG_0
	MOVD a0+16(FP), R0
#endif
#ifdef WAGO_INT_FIXED_ARG_1
	MOVD a1+24(FP), R1
#endif
#ifdef WAGO_INT_FIXED_ARG_2
	MOVD a2+32(FP), R2
#endif
#ifdef WAGO_INT_FIXED_ARG_3
	MOVD a3+40(FP), R3
#endif
	MOVD R10, RSP
	MOVD ZR, R29
	MOVD R10, -24(R26)
	ADR  afterNativeIntLightCall, R11
	MOVD R11, -32(R26)
	BL   (R9)

afterNativeIntLightCall:
	MOVD 8(RSP), R26
	LDP  16(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
#ifdef WAGO_INT_FIXED_RESULT
	MOVD R0, ret+56(FP)
#else
	MOVD ZR, ret+56(FP)
#endif
	RET

// func enterNativeIntPreboundContextRaw(call *PreparedIntCall, a0, a1, a2, a3 uintptr) uintptr
// The prepared block fixes code, linear memory, and foreign stack while values
// remain direct Go ABI arguments. This is the ARM64 counterpart of AMD64's
// prebound-context entry.
TEXT ·enterNativeIntPreboundContextRaw(SB), NOSPLIT, $0-48
	MOVD call+0(FP), R12
	MOVD 0(R12), R9
	MOVD 16(R12), R10
	SUB  $32, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	MOVD R26, 8(R10)
	STP  (R29, R30), 16(R10)

	MOVD 8(R12), R26
#ifdef WAGO_INT_FIXED_ARG_0
	MOVD a0+8(FP), R0
#endif
#ifdef WAGO_INT_FIXED_ARG_1
	MOVD a1+16(FP), R1
#endif
#ifdef WAGO_INT_FIXED_ARG_2
	MOVD a2+24(FP), R2
#endif
#ifdef WAGO_INT_FIXED_ARG_3
	MOVD a3+32(FP), R3
#endif
	MOVD R10, RSP
	MOVD ZR, R29
	MOVD R10, -24(R26)
	ADR  afterNativeIntPreboundContext, R11
	MOVD R11, -32(R26)
	BL   (R9)

afterNativeIntPreboundContext:
	MOVD 8(RSP), R26
	LDP  16(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
#ifdef WAGO_INT_FIXED_RESULT
	MOVD R0, ret+40(FP)
#else
	MOVD ZR, ret+40(FP)
#endif
	RET

// func enterNativeIntCallRaw(call *PreparedIntCall) uintptr
// The owning prepared handle admits only the caller-clobber-only light ABI.
TEXT ·enterNativeIntCallRaw(SB), NOSPLIT, $0-16
	MOVD call+0(FP), R12
	MOVD 0(R12), R9
	MOVD 16(R12), R10
	SUB  $32, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	MOVD R26, 8(R10)
	STP  (R29, R30), 16(R10)

	MOVD 8(R12), R26
#ifdef WAGO_INT_FIXED_ARG_0
	MOVD 24(R12), R0
#endif
#ifdef WAGO_INT_FIXED_ARG_1
	MOVD 32(R12), R1
#endif
#ifdef WAGO_INT_FIXED_ARG_2
	MOVD 40(R12), R2
#endif
#ifdef WAGO_INT_FIXED_ARG_3
	MOVD 48(R12), R3
#endif
	MOVD R10, RSP
	MOVD ZR, R29
	MOVD R10, -24(R26)
	ADR  afterNativeIntCallBlock, R11
	MOVD R11, -32(R26)
	BL   (R9)

afterNativeIntCallBlock:
	MOVD 8(RSP), R26
	LDP  16(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
#ifdef WAGO_INT_FIXED_RESULT
	MOVD R0, ret+8(FP)
#else
	MOVD ZR, ret+8(FP)
#endif
	RET
