//go:build (linux || darwin) && arm64 && !tinygo

#include "textflag.h"
#include "funcdata.h"
#include "go_asm.h"

TEXT ·inlineHostMultiViewEnter(SB), NOSPLIT, $96-96
	NO_LOCAL_POINTERS
	MOVD ZR, ret+88(FP)
	MOVD code+8(FP), R9
	MOVD args+16(FP), R0
	MOVD linMem+24(FP), R1
	MOVD trap+32(FP), R2
	MOVD results+40(FP), R3
	MOVD stack+48(FP), R10
	ADR multiViewCallback, R12
	BL ·inlineHostEnterForeign(SB)
	RET
multiViewCallback:
	MOVD ctrl+56(FP), R10
	MOVD 64(RSP), R11
	CMP R10, R11
	BNE multiViewAbortAdmission
	MOVWU const_hcImportIdx(R10), R11
	MOVD signatures_len+72(FP), R12
	CMP R12, R11
	BHS multiViewAbortAdmission
	MOVD signatures_base+64(FP), R12
	ADD R11<<2, R12, R12
	MOVWU 0(R12), R12
	MOVWU const_hcNArgs(R10), R11
	CMP R12, R11
	BNE multiViewAbortAdmission
	MOVD trap+32(FP), R9
	MOVWU 0(R9), R11
	CBNZ R11, multiViewAbortTrap
	MOVWU const_hcNArgs(R10), R9
	LSR $16, R9, R4
	ADD $const_hcResults, R10, R3
	MOVD R4, R5
	MOVD R3, R11
	MOVD R4, R12
	CBZ R12, multiViewResultsCleared
multiViewClearResults:
	MOVD.P ZR, 8(R11)
	SUB $1, R12, R12
	CBNZ R12, multiViewClearResults
multiViewResultsCleared:
	AND $65535, R9, R1
	MOVD R1, R2
	ADD $const_hcArgs, R10, R0
	MOVD fn+0(FP), R26
	MOVD 0(R26), R9
	BL (R9)
	MOVD trap+32(FP), R9
	MOVWU 0(R9), R11
	CBNZ R11, multiViewAbortTrap
	MOVD ctrl+56(FP), R10
	MOVD $·inlineHostViewStagedBridge(SB), R9
	MOVD R9, const_hcTrampoline(R10)
	MOVD 72(RSP), R9
	MOVD 64(RSP), R11
	JMP ·inlineHostResumeForeign(SB)
multiViewAbortAdmission:
	MOVD $1, R9
	MOVD R9, ret+88(FP)
	RET
multiViewAbortTrap:
	MOVD $2, R9
	MOVD R9, ret+88(FP)
	RET
