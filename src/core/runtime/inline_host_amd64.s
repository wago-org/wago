//go:build (linux || darwin) && amd64 && !tinygo

#include "textflag.h"
#include "funcdata.h"
#include "go_asm.h"

TEXT ·inlineHostEnter(SB), NOSPLIT, $64-80
	NO_LOCAL_POINTERS
	MOVQ $0, ret+72(FP)
	MOVQ code+8(FP), R11
	MOVQ args+16(FP), DI
	MOVQ linMem+24(FP), SI
	MOVQ trap+32(FP), DX
	MOVQ results+40(FP), CX
	MOVQ stack+48(FP), R10
	CALL ·inlineHostEnterForeign(SB)
	TESTQ AX, AX
	JNE scalarCallback
	RET

// This label is in the normal Go owner; a native import restores its SP/BP/g
// before jumping here. The typed fn argument map remains visible to stack GC.
scalarCallback:
	MOVQ ctrl+56(FP), R10
	CMPQ 32(SP), R10
	JNE scalarAbortAdmission
	MOVL slots+64(FP), R11
	SHLQ $32, R11
	CMPQ const_hcImportIdx(R10), R11
	JNE scalarAbortAdmission
	MOVQ trap+32(FP), R9
	CMPL 0(R9), $0
	JNE scalarAbortTrap
	MOVQ fn+0(FP), DX
	MOVQ 0(DX), R9
	MOVQ const_hcArgs(R10), AX
	MOVQ const_hcArgs+8(R10), BX
	CALL R9
	MOVQ trap+32(FP), R9
	CMPL 0(R9), $0
	JNE scalarAbortTrap
	MOVQ ctrl+56(FP), R10
	MOVL slots+64(FP), R11
	SHRL $16, R11
	TESTL R11, R11
	JE scalarResultDone
	CMPL R11, $1
	JNE scalarTwoResults
	MOVQ AX, const_hcResults(R10)
	JMP scalarResultDone
scalarTwoResults:
	MOVL AX, R12
	SHRQ $32, AX
	MOVQ R12, const_hcResults(R10)
	MOVQ AX, const_hcResults+8(R10)
scalarResultDone:
	LEAQ ·inlineHostStagedBridge(SB), R11
	MOVQ R11, const_hcTrampoline(R10)
	MOVQ 40(SP), R10
	MOVQ 32(SP), R9
	JMP ·inlineHostResumeForeign(SB)
scalarAbortAdmission:
	MOVQ $1, ret+72(FP)
	RET
scalarAbortTrap:
	MOVQ $2, ret+72(FP)
	RET

// Landing record: Go SP/BP 0/8, g 40, host/guest MXCSR 56/60,
// Go owner continuation PC 64. Its callback/completion discriminator is AX.
// The normal owner never writes SP. Only these foreign helpers switch stacks.
TEXT ·inlineHostEnterForeign(SB), NOSPLIT|NOFRAME, $0-0
	SUBQ $96, R10
	LEAQ 8(SP), R9
	MOVQ R9, 0(R10)
	MOVQ BP, 8(R10)
	MOVQ R14, 40(R10)
	MOVQ 0(SP), R9
	MOVQ R9, 64(R10)
	STMXCSR 56(R10)
	MOVL $0x1f80, 60(R10)
	LDMXCSR 60(R10)
	LEAQ -8(R10), AX
	MOVQ AX, -const_offTrapStackReentry(SI)
	MOVQ R10, SP
	XORL BP, BP
	CALL R11
nativeReturn:
	LDMXCSR 56(SP)
	MOVQ 8(SP), BP
	MOVQ 40(SP), R14
	MOVQ 64(SP), R11
	MOVQ 0(SP), SP
	PXOR X15, X15
	XORL AX, AX
	JMP R11

TEXT ·inlineHostResumeForeign(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ SP, 0(R10)
	MOVQ BP, 8(R10)
	MOVQ R14, 40(R10)
	STMXCSR 56(R10)
	LDMXCSR 60(R10)
	MOVQ const_hcSavedRBX(R9), R11
	LEAQ -8(R10), AX
	MOVQ AX, -const_offTrapStackReentry(R11)
	MOVQ const_hcSavedRBX(R9), BX
	MOVQ const_hcSavedRBP(R9), BP
	MOVQ const_hcSavedR12(R9), R12
	MOVQ const_hcSavedR13(R9), R13
	MOVQ const_hcSavedR14(R9), R14
	MOVQ const_hcSavedR15(R9), R15
	MOVQ const_hcSavedRSP(R9), SP
	RET

TEXT ·inlineHostStagedBridge(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ -const_offCustomCtx(BX), R9
	MOVQ SP, const_hcSavedRSP(R9)
	MOVQ BX, const_hcSavedRBX(R9)
	MOVQ BP, const_hcSavedRBP(R9)
	MOVQ R12, const_hcSavedR12(R9)
	MOVQ R13, const_hcSavedR13(R9)
	MOVQ R14, const_hcSavedR14(R9)
	MOVQ R15, const_hcSavedR15(R9)
	MOVQ -const_inlineHostTrapCellOffset(BX), R10
	MOVQ R9, 8(R10)
	MOVQ -const_offTrapStackReentry(BX), R10
	ADDQ $8, R10
	MOVQ 0(R10), R11
	MOVQ R9, 32(R11)
	MOVQ R10, 40(R11)
	MOVQ 8(R10), BP
	MOVQ 40(R10), R14
	LDMXCSR 56(R10)
	MOVQ 64(R10), R9
	MOVQ R11, SP
	PXOR X15, X15
	MOVQ $1, AX
	JMP R9

TEXT ·inlineHostBridgeAddr(SB), NOSPLIT|NOFRAME, $0-8
	LEAQ ·inlineHostStagedBridge(SB), AX
	MOVQ AX, ret+0(FP)
	RET

// The six slice ABI registers spill into the first 48 bytes of the owner.
// Parked metadata lives at 64/72(SP), beyond that outgoing spill space.
TEXT ·inlineHostViewEnter(SB), NOSPLIT, $96-80
	NO_LOCAL_POINTERS
	MOVQ $0, ret+72(FP)
	MOVQ code+8(FP), R11
	MOVQ args+16(FP), DI
	MOVQ linMem+24(FP), SI
	MOVQ trap+32(FP), DX
	MOVQ results+40(FP), CX
	MOVQ stack+48(FP), R10
	CALL ·inlineHostEnterForeign(SB)
	TESTQ AX, AX
	JNE viewCallback
	RET
viewCallback:
	MOVQ ctrl+56(FP), R10
	CMPQ 64(SP), R10
	JNE viewAbortAdmission
	MOVL slots+64(FP), R11
	SHLQ $32, R11
	CMPQ const_hcImportIdx(R10), R11
	JNE viewAbortAdmission
	MOVQ trap+32(FP), R9
	CMPL 0(R9), $0
	JNE viewAbortTrap
	MOVL slots+64(FP), R9
	MOVL R9, SI
	SHRL $16, SI
	MOVQ SI, R8
	LEAQ const_hcResults(R10), DI
	MOVQ DI, R11
	MOVQ SI, R12
	TESTQ R12, R12
	JE viewResultsCleared
viewClearResults:
	MOVQ $0, 0(R11)
	ADDQ $8, R11
	DECQ R12
	JNE viewClearResults
viewResultsCleared:
	ANDL $65535, R9
	MOVQ R9, BX
	MOVQ R9, CX
	LEAQ const_hcArgs(R10), AX
	MOVQ fn+0(FP), DX
	MOVQ 0(DX), R9
	CALL R9
	MOVQ trap+32(FP), R9
	CMPL 0(R9), $0
	JNE viewAbortTrap
	MOVQ ctrl+56(FP), R10
	LEAQ ·inlineHostViewStagedBridge(SB), R11
	MOVQ R11, const_hcTrampoline(R10)
	MOVQ 72(SP), R10
	MOVQ 64(SP), R9
	JMP ·inlineHostResumeForeign(SB)
viewAbortAdmission:
	MOVQ $1, ret+72(FP)
	RET
viewAbortTrap:
	MOVQ $2, ret+72(FP)
	RET

TEXT ·inlineHostViewStagedBridge(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ -const_offCustomCtx(BX), R9
	MOVQ SP, const_hcSavedRSP(R9)
	MOVQ BX, const_hcSavedRBX(R9)
	MOVQ BP, const_hcSavedRBP(R9)
	MOVQ R12, const_hcSavedR12(R9)
	MOVQ R13, const_hcSavedR13(R9)
	MOVQ R14, const_hcSavedR14(R9)
	MOVQ R15, const_hcSavedR15(R9)
	MOVQ -const_inlineHostTrapCellOffset(BX), R10
	MOVQ R9, 8(R10)
	MOVQ -const_offTrapStackReentry(BX), R10
	ADDQ $8, R10
	MOVQ 0(R10), R11
	MOVQ R9, 64(R11)
	MOVQ R10, 72(R11)
	MOVQ 8(R10), BP
	MOVQ 40(R10), R14
	LDMXCSR 56(R10)
	MOVQ 64(R10), R9
	MOVQ R11, SP
	PXOR X15, X15
	MOVQ $1, AX
	JMP R9

TEXT ·inlineHostViewBridgeAddr(SB), NOSPLIT|NOFRAME, $0-8
	LEAQ ·inlineHostViewStagedBridge(SB), AX
	MOVQ AX, ret+0(FP)
	RET


// Static typed callback target with an explicit rooted Go context.
TEXT ·inlineHostContextEnter(SB), NOSPLIT, $64-88
	NO_LOCAL_POINTERS
	MOVQ $0, ret+80(FP)
	MOVQ code+16(FP), R11
	MOVQ args+24(FP), DI
	MOVQ linMem+32(FP), SI
	MOVQ trap+40(FP), DX
	MOVQ results+48(FP), CX
	MOVQ stack+56(FP), R10
	CALL ·inlineHostEnterForeign(SB)
	TESTQ AX, AX
	JNE context_scalarCallback
	RET

// This label is in the normal Go owner; a native import restores its SP/BP/g
// before jumping here. The typed fn argument map remains visible to stack GC.
context_scalarCallback:
	MOVQ ctrl+64(FP), R10
	CMPQ 32(SP), R10
	JNE context_scalarAbortAdmission
	MOVL slots+72(FP), R11
	SHLQ $32, R11
	CMPQ const_hcImportIdx(R10), R11
	JNE context_scalarAbortAdmission
	MOVQ trap+40(FP), R9
	CMPL 0(R9), $0
	JNE context_scalarAbortTrap
	MOVQ fn+0(FP), DX
	MOVQ 0(DX), R9
	MOVQ const_hcArgs(R10), BX
	MOVQ const_hcArgs+8(R10), CX
	MOVQ context+8(FP), AX
	CALL R9
	MOVQ trap+40(FP), R9
	CMPL 0(R9), $0
	JNE context_scalarAbortTrap
	MOVQ ctrl+64(FP), R10
	MOVL slots+72(FP), R11
	SHRL $16, R11
	TESTL R11, R11
	JE context_scalarResultDone
	CMPL R11, $1
	JNE context_scalarTwoResults
	MOVQ AX, const_hcResults(R10)
	JMP context_scalarResultDone
context_scalarTwoResults:
	MOVL AX, R12
	SHRQ $32, AX
	MOVQ R12, const_hcResults(R10)
	MOVQ AX, const_hcResults+8(R10)
context_scalarResultDone:
	LEAQ ·inlineHostStagedBridge(SB), R11
	MOVQ R11, const_hcTrampoline(R10)
	MOVQ 40(SP), R10
	MOVQ 32(SP), R9
	JMP ·inlineHostResumeForeign(SB)
context_scalarAbortAdmission:
	MOVQ $1, ret+80(FP)
	RET
context_scalarAbortTrap:
	MOVQ $2, ret+80(FP)
	RET

// Static target with a rooted Go context; seven outgoing argument words.
TEXT ·inlineHostViewContextEnter(SB), NOSPLIT, $96-88
	NO_LOCAL_POINTERS
	MOVQ $0, ret+80(FP)
	MOVQ code+16(FP), R11
	MOVQ args+24(FP), DI
	MOVQ linMem+32(FP), SI
	MOVQ trap+40(FP), DX
	MOVQ results+48(FP), CX
	MOVQ stack+56(FP), R10
	CALL ·inlineHostEnterForeign(SB)
	TESTQ AX, AX
	JNE context_viewCallback
	RET
context_viewCallback:
	MOVQ ctrl+64(FP), R10
	CMPQ 64(SP), R10
	JNE context_viewAbortAdmission
	MOVL slots+72(FP), R11
	SHLQ $32, R11
	CMPQ const_hcImportIdx(R10), R11
	JNE context_viewAbortAdmission
	MOVQ trap+40(FP), R9
	CMPL 0(R9), $0
	JNE context_viewAbortTrap
	MOVL slots+72(FP), R9
	MOVL R9, R8
	SHRL $16, R8
	MOVQ R8, R9
	LEAQ const_hcResults(R10), SI
	MOVQ SI, R11
	MOVQ R8, R12
	TESTQ R12, R12
	JE context_viewResultsCleared
context_viewClearResults:
	MOVQ $0, 0(R11)
	ADDQ $8, R11
	DECQ R12
	JNE context_viewClearResults
context_viewResultsCleared:
	MOVL slots+72(FP), CX
	ANDL $65535, CX
	MOVQ CX, DI
	LEAQ const_hcArgs(R10), BX
	MOVQ context+8(FP), AX
	MOVQ fn+0(FP), DX
	MOVQ 0(DX), R11
	CALL R11
	MOVQ trap+40(FP), R9
	CMPL 0(R9), $0
	JNE context_viewAbortTrap
	MOVQ ctrl+64(FP), R10
	LEAQ ·inlineHostViewStagedBridge(SB), R11
	MOVQ R11, const_hcTrampoline(R10)
	MOVQ 72(SP), R10
	MOVQ 64(SP), R9
	JMP ·inlineHostResumeForeign(SB)
context_viewAbortAdmission:
	MOVQ $1, ret+80(FP)
	RET
context_viewAbortTrap:
	MOVQ $2, ret+80(FP)
	RET
