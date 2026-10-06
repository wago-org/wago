//go:build (linux || darwin) && amd64 && !tinygo

#include "textflag.h"
#include "funcdata.h"
#include "go_asm.h"

// Integer-only, resource-free guest contexts never observe or change MXCSR.
// Go stack/root/trap/register contracts match the ordinary owner. The view
// owner consumes a single immutable import shape proved before admission.
TEXT ·inlineHostIntegerEnterForeign(SB), NOSPLIT|NOFRAME, $0-0
	SUBQ $96, R10
	LEAQ 8(SP), R9
	MOVQ R9, 0(R10)
	MOVQ BP, 8(R10)
	MOVQ R14, 40(R10)
	MOVQ 0(SP), R9
	MOVQ R9, 64(R10)
	LEAQ -8(R10), AX
	MOVQ AX, -const_offTrapStackReentry(SI)
	MOVQ R10, SP
	XORL BP, BP
	CALL R11
integer_nativeReturn:
	MOVQ 8(SP), BP
	MOVQ 40(SP), R14
	MOVQ 64(SP), R11
	MOVQ 0(SP), SP
	PXOR X15, X15
	XORL AX, AX
	JMP R11

TEXT ·inlineHostIntegerResumeForeign(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ SP, 0(R10)
	MOVQ BP, 8(R10)
	MOVQ R14, 40(R10)
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

TEXT ·inlineHostIntegerStagedBridge(SB), NOSPLIT|NOFRAME, $0-0
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
	MOVQ 64(R10), R9
	MOVQ R11, SP
	PXOR X15, X15
	MOVQ $1, AX
	JMP R9

TEXT ·inlineHostIntegerBridgeAddr(SB), NOSPLIT|NOFRAME, $0-8
	LEAQ ·inlineHostIntegerStagedBridge(SB), AX
	MOVQ AX, ret+0(FP)
	RET

// The six slice ABI registers spill into the first 48 bytes of the owner.
// Parked metadata lives at 64/72(SP), beyond that outgoing spill space.

TEXT ·inlineHostIntegerViewStagedBridge(SB), NOSPLIT|NOFRAME, $0-0
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
	MOVQ 64(R10), R9
	MOVQ R11, SP
	PXOR X15, X15
	MOVQ $1, AX
	JMP R9

TEXT ·inlineHostIntegerViewBridgeAddr(SB), NOSPLIT|NOFRAME, $0-8
	LEAQ ·inlineHostIntegerViewStagedBridge(SB), AX
	MOVQ AX, ret+0(FP)
	RET


// Static typed callback target with an explicit rooted Go context.

TEXT ·inlineHostIntegerContextEnter(SB), NOSPLIT, $64-88
	NO_LOCAL_POINTERS
	MOVQ $0, ret+80(FP)
	MOVQ code+16(FP), R11
	MOVQ args+24(FP), DI
	MOVQ linMem+32(FP), SI
	MOVQ trap+40(FP), DX
	MOVQ results+48(FP), CX
	MOVQ stack+56(FP), R10
	CALL ·inlineHostIntegerEnterForeign(SB)
	TESTQ AX, AX
	JNE integer_context_scalarCallback
	RET

// This label is in the normal Go owner; a native import restores its SP/BP/g
// before jumping here. The typed fn argument map remains visible to stack GC.
integer_context_scalarCallback:
	MOVQ ctrl+64(FP), R10
	CMPQ 32(SP), R10
	JNE integer_context_scalarAbortAdmission
	MOVL slots+72(FP), R11
	SHLQ $32, R11
	CMPQ const_hcImportIdx(R10), R11
	JNE integer_context_scalarAbortAdmission
	MOVQ trap+40(FP), R9
	CMPL 0(R9), $0
	JNE integer_context_scalarAbortTrap
	MOVQ fn+0(FP), DX
	MOVQ 0(DX), R9
	MOVQ const_hcArgs(R10), BX
	MOVQ const_hcArgs+8(R10), CX
	MOVQ context+8(FP), AX
	CALL R9
	MOVQ trap+40(FP), R9
	CMPL 0(R9), $0
	JNE integer_context_scalarAbortTrap
	MOVQ ctrl+64(FP), R10
	MOVL slots+72(FP), R11
	SHRL $16, R11
	TESTL R11, R11
	JE integer_context_scalarResultDone
	CMPL R11, $1
	JNE integer_context_scalarTwoResults
	MOVQ AX, const_hcResults(R10)
	JMP integer_context_scalarResultDone
integer_context_scalarTwoResults:
	MOVL AX, R12
	SHRQ $32, AX
	MOVQ R12, const_hcResults(R10)
	MOVQ AX, const_hcResults+8(R10)
integer_context_scalarResultDone:
	LEAQ ·inlineHostIntegerStagedBridge(SB), R11
	MOVQ R11, const_hcTrampoline(R10)
	MOVQ 40(SP), R10
	MOVQ 32(SP), R9
	JMP ·inlineHostIntegerResumeForeign(SB)
integer_context_scalarAbortAdmission:
	MOVQ $1, ret+80(FP)
	RET
integer_context_scalarAbortTrap:
	MOVQ $2, ret+80(FP)
	RET

// Static target with a rooted Go context; seven outgoing argument words.

TEXT ·inlineHostIntegerViewContextEnter(SB), NOSPLIT, $96-88
	NO_LOCAL_POINTERS
	MOVQ $0, ret+80(FP)
	MOVQ code+16(FP), R11
	MOVQ args+24(FP), DI
	MOVQ linMem+32(FP), SI
	MOVQ trap+40(FP), DX
	MOVQ results+48(FP), CX
	MOVQ stack+56(FP), R10
	CALL ·inlineHostIntegerEnterForeign(SB)
	TESTQ AX, AX
	JNE integer_context_viewCallback
	RET
integer_context_viewCallback:
	MOVQ ctrl+64(FP), R10
	CMPQ 64(SP), R10
	JNE integer_context_viewAbortAdmission
	// Validated single-import integer code has this immutable slot shape.
	MOVQ trap+40(FP), R9
	CMPL 0(R9), $0
	JNE integer_context_viewAbortTrap
	MOVL slots+72(FP), R9
	MOVL R9, R8
	SHRL $16, R8
	MOVQ R8, R9
	LEAQ const_hcResults(R10), SI
	MOVQ SI, R11
	MOVQ R8, R12
	TESTQ R12, R12
	JE integer_context_viewResultsCleared
integer_context_viewClearResults:
	MOVQ $0, 0(R11)
	ADDQ $8, R11
	DECQ R12
	JNE integer_context_viewClearResults
integer_context_viewResultsCleared:
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
	JNE integer_context_viewAbortTrap
	MOVQ ctrl+64(FP), R10
	LEAQ ·inlineHostIntegerViewStagedBridge(SB), R11
	MOVQ R11, const_hcTrampoline(R10)
	MOVQ 72(SP), R10
	MOVQ 64(SP), R9
	JMP ·inlineHostIntegerResumeForeign(SB)
integer_context_viewAbortAdmission:
	MOVQ $1, ret+80(FP)
	RET
integer_context_viewAbortTrap:
	MOVQ $2, ret+80(FP)
	RET


TEXT ·inlineHostIntegerI32Enter(SB), NOSPLIT, $64-80
	NO_LOCAL_POINTERS
	MOVQ $0, ret+72(FP)
	MOVQ code+8(FP), R11
	MOVQ args+16(FP), DI
	MOVQ linMem+24(FP), SI
	MOVQ trap+32(FP), DX
	MOVQ results+40(FP), CX
	MOVQ stack+48(FP), R10
	CALL ·inlineHostIntegerEnterForeign(SB)
	TESTQ AX, AX
	JNE integer_direct_i32Callback
	RET

// This label is in the normal Go owner; a native import restores its SP/BP/g
// before jumping here. The typed fn argument map remains visible to stack GC.
integer_direct_i32Callback:
	MOVQ ctrl+56(FP), R10
	CMPQ 32(SP), R10
	JNE integer_direct_i32AbortAdmission
	MOVL slots+64(FP), R11
	SHLQ $32, R11
	CMPQ const_hcImportIdx(R10), R11
	JNE integer_direct_i32AbortAdmission
	MOVQ trap+32(FP), R9
	CMPL 0(R9), $0
	JNE integer_direct_i32AbortTrap
	MOVQ fn+0(FP), DX
	MOVQ 0(DX), R9
	MOVL const_hcArgs(R10), AX
	CALL R9
	MOVQ trap+32(FP), R9
	CMPL 0(R9), $0
	JNE integer_direct_i32AbortTrap
	MOVQ ctrl+56(FP), R10
	MOVL AX, AX
	MOVQ AX, const_hcResults(R10)
integer_direct_i32ResultDone:
	LEAQ ·inlineHostIntegerStagedBridge(SB), R11
	MOVQ R11, const_hcTrampoline(R10)
	MOVQ 40(SP), R10
	MOVQ 32(SP), R9
	JMP ·inlineHostIntegerResumeForeign(SB)
integer_direct_i32AbortAdmission:
	MOVQ $1, ret+72(FP)
	RET
integer_direct_i32AbortTrap:
	MOVQ $2, ret+72(FP)
	RET
