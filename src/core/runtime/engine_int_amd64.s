//go:build amd64 && !tinygo

#include "textflag.h"

// func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr
TEXT ·enterNativeIntRaw(SB), NOSPLIT, $0-64
	MOVQ code+0(FP), R11
	MOVQ foreignStackTop+48(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), SI
	MOVQ SI, -24(BX)
	MOVQ a0+16(FP), AX
	MOVQ a1+24(FP), CX
	MOVQ a2+32(FP), DX
	MOVQ a3+40(FP), R8

	MOVQ R10, SP
	XORL BP, BP
	CALL R11
	MOVQ AX, DI

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

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), SI
	MOVQ SI, -24(BX)
	MOVQ a0+16(FP), AX
	MOVQ a1+24(FP), CX
	MOVQ a2+32(FP), DX
	MOVQ a3+40(FP), R8

	MOVQ R10, SP
	XORL BP, BP
	CALL R11

	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
	MOVQ AX, ret+56(FP)
	MOVQ DX, ret1+64(FP)
	RET

// func enterNativeIntWideRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr)
// The compiler's integer register ABI admits seven parameters on amd64.
TEXT ·enterNativeIntWideRaw(SB), NOSPLIT, $0-48
	MOVQ code+0(FP), SI
	MOVQ foreignStackTop+24(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), R9
	MOVQ R9, -24(BX)
	MOVQ args+16(FP), DI
	MOVQ R10, SP
	MOVQ  0(DI), AX
	MOVQ  8(DI), CX
	MOVQ 16(DI), DX
	MOVQ 24(DI), R8
	MOVQ 32(DI), R9
	MOVQ 40(DI), R10
	MOVQ 48(DI), R11
	XORL BP, BP
	CALL SI

	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
	MOVQ AX, ret+32(FP)
	MOVQ DX, ret1+40(FP)
	RET

// func enterNativeFloatRaw(code, linMem uintptr, args *[4]uint64, foreignStackTop uintptr) uintptr
// Float-only parameters use XMM0..XMM3; the result returns in XMM0.
TEXT ·enterNativeFloatRaw(SB), NOSPLIT, $0-40
	MOVQ code+0(FP), SI
	MOVQ foreignStackTop+24(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), R9
	MOVQ R9, -24(BX)
	MOVQ args+16(FP), DI
	MOVQ R10, SP
	MOVQ  0(DI), X0
	MOVQ  8(DI), X1
	MOVQ 16(DI), X2
	MOVQ 24(DI), X3
	XORL BP, BP
	CALL SI
	MOVQ X0, AX

	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
	MOVQ AX, ret+32(FP)
	RET

// func enterNativeMixedRaw(code, linMem uintptr, args *[8]uint64, foreignStackTop uintptr) (uintptr, uintptr, uintptr)
// GP args occupy RAX/RCX/RDX/R8; FP args independently occupy XMM0..XMM3.
TEXT ·enterNativeMixedRaw(SB), NOSPLIT, $0-56
	MOVQ code+0(FP), SI
	MOVQ foreignStackTop+24(FP), R10
	SUBQ $32, R10
	MOVQ SP,  0(R10)
	MOVQ BX,  8(R10)
	MOVQ BP, 16(R10)

	MOVQ linMem+8(FP), BX
	LEAQ -8(R10), R9
	MOVQ R9, -24(BX)
	MOVQ args+16(FP), DI
	MOVQ R10, SP
	MOVQ 32(DI), X0
	MOVQ 40(DI), X1
	MOVQ 48(DI), X2
	MOVQ 56(DI), X3
	MOVQ  0(DI), AX
	MOVQ  8(DI), CX
	MOVQ 16(DI), DX
	MOVQ 24(DI), R8
	XORL BP, BP
	CALL SI
	MOVQ X0, R9

	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
	MOVQ AX, ret+32(FP)
	MOVQ DX, ret1+40(FP)
	MOVQ R9, ret2+48(FP)
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

	MOVQ 8(R9), BX
	MOVQ R10, -24(BX)
	MOVQ a0+8(FP), AX
	MOVQ a1+16(FP), CX
	MOVQ a2+24(FP), DX
	MOVQ a3+32(FP), R8

	LEAQ 8(R10), SP
	XORL BP, BP
	CALL R11
	MOVQ AX, DI

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

	MOVQ  8(R9), BX
	LEAQ -8(R10), SI
	MOVQ SI, -24(BX)
	MOVQ 24(R9), AX
	MOVQ 32(R9), CX
	MOVQ 40(R9), DX
	MOVQ 48(R9), R8

	MOVQ R10, SP
	XORL BP, BP
	CALL R11

	MOVQ 16(SP), BP
	MOVQ  8(SP), BX
	MOVQ  0(SP), SP
	PXOR X15, X15
	MOVQ AX, ret+8(FP)
	RET
