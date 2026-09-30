//go:build amd64 && !tinygo

#include "textflag.h"

DATA ·narrowAVX2Mask<>+0(SB)/8, $0x00000000ffffffff
DATA ·narrowAVX2Mask<>+8(SB)/8, $0x00000000ffffffff
DATA ·narrowAVX2Mask<>+16(SB)/8, $0x00000000ffffffff
DATA ·narrowAVX2Mask<>+24(SB)/8, $0x00000000ffffffff
GLOBL ·narrowAVX2Mask<>(SB), RODATA|NOPTR, $32

// copyNarrowScalarSlotsAVX2 clears each slot's high 32 bits, eight at a time.
// Callers gate this instruction stream on CPU AVX2 and enabled OS AVX state.
TEXT ·copyNarrowScalarSlotsAVX2(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ n+16(FP), CX
	VMOVDQU ·narrowAVX2Mask<>(SB), Y2
	CMPQ CX, $8
	JB tail4
loop8:
	VMOVDQU (SI), Y0
	VMOVDQU 32(SI), Y1
	VPAND Y2, Y0, Y0
	VPAND Y2, Y1, Y1
	VMOVDQU Y0, (DI)
	VMOVDQU Y1, 32(DI)
	ADDQ $64, SI
	ADDQ $64, DI
	SUBQ $8, CX
	CMPQ CX, $8
	JAE loop8
tail4:
	CMPQ CX, $4
	JB tail
	VMOVDQU (SI), Y0
	VPAND Y2, Y0, Y0
	VMOVDQU Y0, (DI)
	ADDQ $32, SI
	ADDQ $32, DI
	SUBQ $4, CX
tail:
	TESTQ CX, CX
	JZ done
tailLoop:
	MOVL (SI), AX
	MOVQ AX, (DI)
	ADDQ $8, SI
	ADDQ $8, DI
	DECQ CX
	JNZ tailLoop
done:
	VZEROUPPER
	RET
