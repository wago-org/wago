// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo

#include "textflag.h"
#include "standalone_registers.h"

// func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
TEXT ·enterNativeIntRaw(SB), NOSPLIT, $0-64
	MOVQ code+0(FP), R11
	MOVQ foreignStackTop+48(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)
	STMXCSR 24(R10)
	MOVL $0x1f80, 28(R10)
	LDMXCSR 28(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), SI
	MOVQ SI, -24(BX)
#ifdef WAGO_INT_FIXED_ARG_0
	MOVQ a0+16(FP), AX
#endif
#ifdef WAGO_INT_FIXED_ARG_1
	MOVQ a1+24(FP), CX
#endif
#ifdef WAGO_INT_FIXED_ARG_2
	MOVQ a2+32(FP), DX
#endif
#ifdef WAGO_INT_FIXED_ARG_3
	MOVQ a3+40(FP), R8
#endif

	MOVQ R10, SP
	XORL BP, BP
	CALL R11
#ifdef WAGO_INT_FIXED_RESULT
	MOVQ AX, DI
#else
	XORL DI, DI
#endif

	LDMXCSR 24(SP)
	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
	MOVQ DI, ret+56(FP)
	RET

// func enterNativeIntPairRaw(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) (uintptr, uintptr)
// The internal integer ABI returns two values in RAX/RDX.
TEXT ·enterNativeIntPairRaw(SB), NOSPLIT, $0-72
	MOVQ code+0(FP), R11
	MOVQ foreignStackTop+48(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)
	STMXCSR 24(R10)
	MOVL $0x1f80, 28(R10)
	LDMXCSR 28(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), SI
	MOVQ SI, -24(BX)
#ifdef WAGO_INT_PAIR_ARG_0
	MOVQ a0+16(FP), AX
#endif
#ifdef WAGO_INT_PAIR_ARG_1
	MOVQ a1+24(FP), CX
#endif
#ifdef WAGO_INT_PAIR_ARG_2
	MOVQ a2+32(FP), DX
#endif
#ifdef WAGO_INT_PAIR_ARG_3
	MOVQ a3+40(FP), R8
#endif

	MOVQ R10, SP
	XORL BP, BP
	CALL R11

	LDMXCSR 24(SP)
	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
	MOVQ AX, ret+56(FP)
	MOVQ DX, ret1+64(FP)
	RET

// func enterNativeIntWideRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr, uintptr)
// The compiler's integer register ABI admits eight parameters on amd64.
TEXT ·enterNativeIntWideRaw(SB), NOSPLIT, $0-72
	MOVQ code+0(FP), SI
	MOVQ foreignStackTop+24(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)
	STMXCSR 24(R10)
	MOVL $0x1f80, 28(R10)
	LDMXCSR 28(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), R9
	MOVQ R9, -24(BX)
#ifdef WAGO_INT_WIDE_ARGS
	MOVQ args+16(FP), DI
#endif
	MOVQ R10, SP
#ifdef WAGO_INT_WIDE_ARG_0
	MOVQ  0(DI), AX
#endif
#ifdef WAGO_INT_WIDE_ARG_1
	MOVQ  8(DI), CX
#endif
#ifdef WAGO_INT_WIDE_ARG_2
	MOVQ 16(DI), DX
#endif
#ifdef WAGO_INT_WIDE_ARG_3
	MOVQ 24(DI), R8
#endif
#ifdef WAGO_INT_WIDE_ARG_4
	MOVQ 32(DI), R9
#endif
#ifdef WAGO_INT_WIDE_ARG_5
	MOVQ 40(DI), R10
#endif
#ifdef WAGO_INT_WIDE_ARG_6
	MOVQ 48(DI), R11
#endif
#ifdef WAGO_INT_WIDE_ARG_7
	MOVQ 56(DI), DI
#endif
	XORL BP, BP
	CALL SI

	LDMXCSR 24(SP)
	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
#ifndef WAGO_INT_WIDE_RESULT_4
	XORL SI, SI
#endif
#ifdef WAGO_INT_WIDE_RESULT_0
	MOVQ AX, ret+32(FP)
#else
	MOVQ SI, ret+32(FP)
#endif
#ifdef WAGO_INT_WIDE_RESULT_1
	MOVQ DX, ret1+40(FP)
#else
	MOVQ SI, ret1+40(FP)
#endif
#ifdef WAGO_INT_WIDE_RESULT_2
	MOVQ CX, ret2+48(FP)
#else
	MOVQ SI, ret2+48(FP)
#endif
#ifdef WAGO_INT_WIDE_RESULT_3
	MOVQ R8, ret3+56(FP)
#else
	MOVQ SI, ret3+56(FP)
#endif
#ifdef WAGO_INT_WIDE_RESULT_4
	MOVQ R9, ret4+64(FP)
#else
	MOVQ SI, ret4+64(FP)
#endif
	RET

// func enterNativeIntOctRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr)
TEXT ·enterNativeIntOctRaw(SB), NOSPLIT, $0-96
	MOVQ code+0(FP), SI
	MOVQ foreignStackTop+24(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)
	STMXCSR 24(R10)
	MOVL $0x1f80, 28(R10)
	LDMXCSR 28(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), R9
	MOVQ R9, -24(BX)
#ifdef WAGO_INT_OCT_ARGS
	MOVQ args+16(FP), DI
#endif
	MOVQ R10, SP
#ifdef WAGO_INT_OCT_ARG_0
	MOVQ  0(DI), AX
#endif
#ifdef WAGO_INT_OCT_ARG_1
	MOVQ  8(DI), CX
#endif
#ifdef WAGO_INT_OCT_ARG_2
	MOVQ 16(DI), DX
#endif
#ifdef WAGO_INT_OCT_ARG_3
	MOVQ 24(DI), R8
#endif
#ifdef WAGO_INT_OCT_ARG_4
	MOVQ 32(DI), R9
#endif
#ifdef WAGO_INT_OCT_ARG_5
	MOVQ 40(DI), R10
#endif
#ifdef WAGO_INT_OCT_ARG_6
	MOVQ 48(DI), R11
#endif
#ifdef WAGO_INT_OCT_ARG_7
	MOVQ 56(DI), DI
#endif
	XORL BP, BP
	CALL SI

	LDMXCSR 24(SP)
	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
#ifndef WAGO_INT_OCT_RESULT_7
	XORL SI, SI
#endif
#ifdef WAGO_INT_OCT_RESULT_0
	MOVQ AX, ret+32(FP)
#else
	MOVQ SI, ret+32(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_1
	MOVQ DX, ret1+40(FP)
#else
	MOVQ SI, ret1+40(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_2
	MOVQ CX, ret2+48(FP)
#else
	MOVQ SI, ret2+48(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_3
	MOVQ R8, ret3+56(FP)
#else
	MOVQ SI, ret3+56(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_4
	MOVQ R9, ret4+64(FP)
#else
	MOVQ SI, ret4+64(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_5
	MOVQ R10, ret5+72(FP)
#else
	MOVQ SI, ret5+72(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_6
	MOVQ R11, ret6+80(FP)
#else
	MOVQ SI, ret6+80(FP)
#endif
#ifdef WAGO_INT_OCT_RESULT_7
	MOVQ DI, ret7+88(FP)
#else
	MOVQ SI, ret7+88(FP)
#endif
	RET

// func enterNativeFloatRaw(code, linMem uintptr, args *[4]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr)
// Float-only parameters use XMM0..XMM3; results return in XMM0..XMM3.
TEXT ·enterNativeFloatRaw(SB), NOSPLIT, $0-64
	MOVQ code+0(FP), SI
	MOVQ foreignStackTop+24(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)
	STMXCSR 24(R10)
	MOVL $0x1f80, 28(R10)
	LDMXCSR 28(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), R9
	MOVQ R9, -24(BX)
#ifdef WAGO_FLOAT_FOUR_ARGS
	MOVQ args+16(FP), DI
#endif
	MOVQ R10, SP
#ifdef WAGO_FLOAT_FOUR_ARG_0
	MOVQ  0(DI), X0
#endif
#ifdef WAGO_FLOAT_FOUR_ARG_1
	MOVQ  8(DI), X1
#endif
#ifdef WAGO_FLOAT_FOUR_ARG_2
	MOVQ 16(DI), X2
#endif
#ifdef WAGO_FLOAT_FOUR_ARG_3
	MOVQ 24(DI), X3
#endif
	XORL BP, BP
	CALL SI
#ifdef WAGO_FLOAT_FOUR_RESULT_0
	MOVQ X0, AX
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_1
	MOVQ X1, DX
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_2
	MOVQ X2, R9
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_3
	MOVQ X3, R10
#endif

	LDMXCSR 24(SP)
	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
#ifndef WAGO_FLOAT_FOUR_RESULT_3
	XORL R11, R11
#endif
	PXOR X15, X15
#ifdef WAGO_FLOAT_FOUR_RESULT_0
	MOVQ AX, ret+32(FP)
#else
	MOVQ R11, ret+32(FP)
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_1
	MOVQ DX, ret1+40(FP)
#else
	MOVQ R11, ret1+40(FP)
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_2
	MOVQ R9, ret2+48(FP)
#else
	MOVQ R11, ret2+48(FP)
#endif
#ifdef WAGO_FLOAT_FOUR_RESULT_3
	MOVQ R10, ret3+56(FP)
#else
	MOVQ R11, ret3+56(FP)
#endif
	RET

// func enterNativeFloatOctRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr)
TEXT ·enterNativeFloatOctRaw(SB), NOSPLIT, $0-96
	MOVQ code+0(FP), SI
	MOVQ foreignStackTop+24(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)
	STMXCSR 24(R10)
	MOVL $0x1f80, 28(R10)
	LDMXCSR 28(R10)
	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), R9
	MOVQ R9, -24(BX)
#ifdef WAGO_FLOAT_OCT_ARGS
	MOVQ args+16(FP), DI
#endif
	MOVQ R10, SP
#ifdef WAGO_FLOAT_OCT_ARG_0
	MOVQ  0(DI), X0
#endif
#ifdef WAGO_FLOAT_OCT_ARG_1
	MOVQ  8(DI), X1
#endif
#ifdef WAGO_FLOAT_OCT_ARG_2
	MOVQ 16(DI), X2
#endif
#ifdef WAGO_FLOAT_OCT_ARG_3
	MOVQ 24(DI), X3
#endif
#ifdef WAGO_FLOAT_OCT_ARG_4
	MOVQ 32(DI), X4
#endif
#ifdef WAGO_FLOAT_OCT_ARG_5
	MOVQ 40(DI), X5
#endif
#ifdef WAGO_FLOAT_OCT_ARG_6
	MOVQ 48(DI), X6
#endif
#ifdef WAGO_FLOAT_OCT_ARG_7
	MOVQ 56(DI), X7
#endif
	XORL BP, BP
	CALL SI
	LDMXCSR 24(SP)
	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
#ifndef WAGO_FLOAT_OCT_RESULT_7
	XORL R11, R11
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_0
	MOVQ X0, ret+32(FP)
#else
	MOVQ R11, ret+32(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_1
	MOVQ X1, ret1+40(FP)
#else
	MOVQ R11, ret1+40(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_2
	MOVQ X2, ret2+48(FP)
#else
	MOVQ R11, ret2+48(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_3
	MOVQ X3, ret3+56(FP)
#else
	MOVQ R11, ret3+56(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_4
	MOVQ X4, ret4+64(FP)
#else
	MOVQ R11, ret4+64(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_5
	MOVQ X5, ret5+72(FP)
#else
	MOVQ R11, ret5+72(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_6
	MOVQ X6, ret6+80(FP)
#else
	MOVQ R11, ret6+80(FP)
#endif
#ifdef WAGO_FLOAT_OCT_RESULT_7
	MOVQ X7, ret7+88(FP)
#else
	MOVQ R11, ret7+88(FP)
#endif
	PXOR X15, X15
	RET

// func enterNativeMixedRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr, uintptr)
// GP args occupy RAX/RCX/RDX/R8; FP args independently occupy XMM0..XMM3.
TEXT ·enterNativeMixedRaw(SB), NOSPLIT, $0-64
	MOVQ code+0(FP), SI
	MOVQ foreignStackTop+24(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)
	STMXCSR 24(R10)
	MOVL $0x1f80, 28(R10)
	LDMXCSR 28(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), R9
	MOVQ R9, -24(BX)
#ifdef WAGO_MIXED_ARGS
	MOVQ args+16(FP), DI
#endif
	MOVQ R10, SP
#ifdef WAGO_MIXED_FP_ARG_0
	MOVQ 32(DI), X0
#endif
#ifdef WAGO_MIXED_FP_ARG_1
	MOVQ 40(DI), X1
#endif
#ifdef WAGO_MIXED_FP_ARG_2
	MOVQ 48(DI), X2
#endif
#ifdef WAGO_MIXED_FP_ARG_3
	MOVQ 56(DI), X3
#endif
#ifdef WAGO_MIXED_GP_ARG_0
	MOVQ  0(DI), AX
#endif
#ifdef WAGO_MIXED_GP_ARG_1
	MOVQ  8(DI), CX
#endif
#ifdef WAGO_MIXED_GP_ARG_2
	MOVQ 16(DI), DX
#endif
#ifdef WAGO_MIXED_GP_ARG_3
	MOVQ 24(DI), R8
#endif
	XORL BP, BP
	CALL SI
#ifdef WAGO_MIXED_FP_RESULT_0
	MOVQ X0, R9
#endif
#ifdef WAGO_MIXED_FP_RESULT_1
	MOVQ X1, R10
#endif

	LDMXCSR 24(SP)
	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
#ifdef WAGO_MIXED_GP_RESULT_0
	MOVQ AX, ret+32(FP)
#else
	MOVQ $0, ret+32(FP)
#endif
#ifdef WAGO_MIXED_GP_RESULT_1
	MOVQ DX, ret1+40(FP)
#else
	MOVQ $0, ret1+40(FP)
#endif
#ifdef WAGO_MIXED_FP_RESULT_0
	MOVQ R9, ret2+48(FP)
#else
	MOVQ $0, ret2+48(FP)
#endif
#ifdef WAGO_MIXED_FP_RESULT_1
	MOVQ R10, ret3+56(FP)
#else
	MOVQ $0, ret3+56(FP)
#endif
	RET

// func enterNativeIntPreboundContextRaw(call *PreparedIntCall, a0, a1, a2, a3 uintptr) uintptr
TEXT ·enterNativeIntPreboundContextRaw(SB), NOSPLIT, $0-48
	MOVQ call+0(FP), R9
	MOVQ  0(R9), R11
	MOVQ 16(R9), R10
	SUBQ $40, R10 // trap reentry address; save area starts eight bytes above it
	MOVQ SP,  8(R10)
	MOVQ BX, 16(R10)
	MOVQ BP, 24(R10)
	STMXCSR 32(R10)
	MOVL $0x1f80, 36(R10)
	LDMXCSR 36(R10)

	MOVQ 8(R9), BX
	MOVQ R10, -24(BX)
#ifdef WAGO_INT_FIXED_ARG_0
	MOVQ a0+8(FP), AX
#endif
#ifdef WAGO_INT_FIXED_ARG_1
	MOVQ a1+16(FP), CX
#endif
#ifdef WAGO_INT_FIXED_ARG_2
	MOVQ a2+24(FP), DX
#endif
#ifdef WAGO_INT_FIXED_ARG_3
	MOVQ a3+32(FP), R8
#endif

	LEAQ 8(R10), SP
	XORL BP, BP
	CALL R11
#ifdef WAGO_INT_FIXED_RESULT
	MOVQ AX, DI
#else
	XORL DI, DI
#endif

	LDMXCSR 24(SP)
	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
	MOVQ DI, ret+40(FP)
	RET

// func enterNativeIntCallRaw(call *PreparedIntCall) uintptr
TEXT ·enterNativeIntCallRaw(SB), NOSPLIT, $0-16
	MOVQ call+0(FP), R9
	MOVQ  0(R9), R11
	MOVQ 16(R9), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)
	STMXCSR 24(R10)
	MOVL $0x1f80, 28(R10)
	LDMXCSR 28(R10)

	MOVQ  8(R9), BX
	LEAQ -8(R10), SI
	MOVQ SI, -24(BX)
#ifdef WAGO_INT_FIXED_ARG_0
	MOVQ 24(R9), AX
#endif
#ifdef WAGO_INT_FIXED_ARG_1
	MOVQ 32(R9), CX
#endif
#ifdef WAGO_INT_FIXED_ARG_2
	MOVQ 40(R9), DX
#endif
#ifdef WAGO_INT_FIXED_ARG_3
	MOVQ 48(R9), R8
#endif

	MOVQ R10, SP
	XORL BP, BP
	CALL R11

	LDMXCSR 24(SP)
	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
#ifdef WAGO_INT_FIXED_RESULT
	MOVQ AX, ret+8(FP)
#else
	MOVQ $0, ret+8(FP)
#endif
	RET
