//go:build (linux || darwin) && amd64 && !tinygo

#include "textflag.h"
#include "funcdata.h"
#include "go_asm.h"

TEXT ·inlineHostMultiViewEnter(SB), NOSPLIT, $96-96
	NO_LOCAL_POINTERS
	MOVQ $0, ret+88(FP)
	MOVQ code+8(FP), R11
	MOVQ args+16(FP), DI
	MOVQ linMem+24(FP), SI
	MOVQ trap+32(FP), DX
	MOVQ results+40(FP), CX
	MOVQ stack+48(FP), R10
	CALL ·inlineHostEnterForeign(SB)
	TESTQ AX, AX
	JNE multiViewCallback
	RET
multiViewCallback:
	MOVQ ctrl+56(FP), R10
	CMPQ 64(SP), R10
	JNE multiViewAbortAdmission
	MOVL const_hcImportIdx(R10), R11
	CMPQ R11, signatures_len+72(FP)
	JAE multiViewAbortAdmission
	MOVQ signatures_base+64(FP), R12
	MOVL (R12)(R11*4), R11
	CMPL const_hcNArgs(R10), R11
	JNE multiViewAbortAdmission
	MOVQ trap+32(FP), R9
	CMPL 0(R9), $0
	JNE multiViewAbortTrap
	MOVL const_hcNArgs(R10), R9
	MOVL R9, SI
	SHRL $16, SI
	MOVQ SI, R8
	LEAQ const_hcResults(R10), DI
	MOVQ DI, R11
	MOVQ SI, R12
	TESTQ R12, R12
	JE multiViewResultsCleared
multiViewClearResults:
	MOVQ $0, 0(R11)
	ADDQ $8, R11
	DECQ R12
	JNE multiViewClearResults
multiViewResultsCleared:
	ANDL $65535, R9
	MOVQ R9, BX
	MOVQ R9, CX
	LEAQ const_hcArgs(R10), AX
	MOVQ fn+0(FP), DX
	MOVQ 0(DX), R9
	CALL R9
	MOVQ trap+32(FP), R9
	CMPL 0(R9), $0
	JNE multiViewAbortTrap
	MOVQ ctrl+56(FP), R10
	LEAQ ·inlineHostViewStagedBridge(SB), R11
	MOVQ R11, const_hcTrampoline(R10)
	MOVQ 72(SP), R10
	MOVQ 64(SP), R9
	JMP ·inlineHostResumeForeign(SB)
multiViewAbortAdmission:
	MOVQ $1, ret+88(FP)
	RET
multiViewAbortTrap:
	MOVQ $2, ret+88(FP)
	RET
