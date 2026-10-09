//go:build (linux || darwin) && arm64 && !tinygo

#include "textflag.h"
#include "funcdata.h"
#include "go_asm.h"

TEXT ·inlineHostEnter(SB), NOSPLIT, $64-80
	NO_LOCAL_POINTERS
	MOVD ZR, ret+72(FP)
	MOVD code+8(FP), R9
	MOVD args+16(FP), R0
	MOVD linMem+24(FP), R1
	MOVD trap+32(FP), R2
	MOVD results+40(FP), R3
	MOVD stack+48(FP), R10
	ADR goCallback, R12
	BL ·inlineHostEnterForeign(SB)
	RET
goCallback:
	MOVD ctrl+56(FP), R10
	MOVD 32(RSP), R11
	CMP R10, R11
	BNE abortAdmission
	MOVD const_hcImportIdx(R10), R11 // import index and raw slots
	MOVWU slots+64(FP), R12
	LSL $32, R12, R12
	CMP R12, R11
	BNE abortAdmission
	MOVD trap+32(FP), R9
	MOVWU 0(R9), R11
	CBNZ R11, abortTrap
	MOVD fn+0(FP), R26
	MOVD 0(R26), R9
	MOVD const_hcArgs(R10), R0
	MOVD const_hcArgs+8(R10), R1
	BL (R9)
	MOVD trap+32(FP), R9
	MOVWU 0(R9), R11
	CBNZ R11, abortTrap
	MOVD ctrl+56(FP), R10
	MOVWU slots+64(FP), R11
	LSR $16, R11, R11
	CBZ R11, resultDone
	CMP $1, R11
	BNE twoResults
	MOVD R0, const_hcResults(R10)
	B resultDone
twoResults:
	MOVWU R0, R12
	LSR $32, R0, R13
	MOVD R12, const_hcResults(R10)
	MOVD R13, const_hcResults+8(R10)
resultDone:
	MOVD $·inlineHostStagedBridge(SB), R9
	MOVD R9, const_hcTrampoline(R10)
	MOVD 40(RSP), R9
	MOVD 32(RSP), R11
	JMP ·inlineHostResumeForeign(SB)
abortAdmission:
	MOVD $1, R9
	MOVD R9, ret+72(FP)
	RET
abortTrap:
	MOVD $2, R9
	MOVD R9, ret+72(FP)
	// Return directly through the normal Go owner epilogue. No foreign
	// continuation executes after cancellation or an admission violation.
	RET

TEXT ·inlineHostEnterForeign(SB), NOSPLIT|NOFRAME, $0-0
	SUB $176, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	MOVD R26, 64(R10)
	MOVD R12, 72(R10)
	STP (R29, R30), 88(R10)
	MOVD R10, -24(R1)
	ADR nativeReturn, R11
	MOVD R11, 80(R10)
	MOVD R11, -32(R1)
	MOVD R10, RSP
	MOVD ZR, R22
	MOVD ZR, R29
	BL (R9)
nativeReturn:
	MOVD 64(RSP), R26
	LDP 88(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
	JMP (R30)

// A callback may run another engine over aliased backing. Its trap landing
// must never replace this live Go owner's continuation when native code resumes.
TEXT ·inlineHostResumeForeign(SB), NOSPLIT|NOFRAME, $0-0
	MOVD RSP, R12
	MOVD R12, 0(R9)               // Go stack may have moved in the callback
	MOVD R29, 88(R9)
	MOVD const_hcSavedLinMem(R11), R12
	MOVD R9, -24(R12)
	MOVD 80(R9), R13
	MOVD R13, -32(R12)
	MOVD const_hcSavedSP(R11), R12
	MOVD R12, RSP
	LDP const_hcSavedX19(R11), (R19, R20)
	LDP const_hcSavedX21(R11), (R21, R22)
	LDP const_hcSavedX23(R11), (R23, R24)
	LDP const_hcSavedX25(R11), (R25, R26)
	MOVD const_hcSavedX27(R11), R27
	LDP const_hcSavedFP(R11), (R29, R30)
	FLDPD const_hcSavedV8(R11), (F8, F9)
	FLDPD const_hcSavedV10(R11), (F10, F11)
	FLDPD const_hcSavedV12(R11), (F12, F13)
	FLDPD const_hcSavedV14(R11), (F14, F15)
	JMP (R30)

// Preserve the complete existing parked control-frame contract. The live Go
// owner replaces trap-return/resume, rather than changing guest root metadata.
TEXT ·inlineHostStagedBridge(SB), NOSPLIT|NOFRAME, $0-0
	MOVD -const_offCustomCtx(R26), R9
	MOVD RSP, R10
	STP (R10, R19), const_hcSavedSP(R9)
	STP (R20, R21), const_hcSavedX20(R9)
	STP (R22, R23), const_hcSavedX22(R9)
	STP (R24, R25), const_hcSavedX24(R9)
	STP (R26, R27), const_hcSavedX26(R9)
	STP (R26, R29), const_hcSavedLinMem(R9)
	MOVD R30, const_hcSavedLR(R9)
	FSTPD (F8, F9), const_hcSavedV8(R9)
	FSTPD (F10, F11), const_hcSavedV10(R9)
	FSTPD (F12, F13), const_hcSavedV12(R9)
	FSTPD (F14, F15), const_hcSavedV14(R9)
	MOVD -const_inlineHostTrapCellOffset(R26), R10
	MOVD R9, 8(R10)
	MOVD R9, R11                 // parked control frame
	MOVD -24(R26), R9            // this entry's foreign landing record
	MOVD 0(R9), R12              // live Go owner SP
	MOVD R11, 32(R12)
	MOVD R9, 40(R12)
	MOVD 88(R9), R29
	MOVD 72(R9), R9
	MOVD R12, RSP
	JMP (R9)

TEXT ·inlineHostBridgeAddr(SB), NOSPLIT|NOFRAME, $0-8
	MOVD $·inlineHostStagedBridge(SB), R0
	MOVD R0, ret+0(FP)
	RET

// Six slice argument registers require 48 bytes of Go ABI spill space.
// Keep the parked native metadata above that space, at 64 and 72(SP).
TEXT ·inlineHostViewEnter(SB), NOSPLIT, $96-80
	NO_LOCAL_POINTERS
	MOVD ZR, ret+72(FP)
	MOVD code+8(FP), R9
	MOVD args+16(FP), R0
	MOVD linMem+24(FP), R1
	MOVD trap+32(FP), R2
	MOVD results+40(FP), R3
	MOVD stack+48(FP), R10
	ADR viewCallback, R12
	BL ·inlineHostEnterForeign(SB)
	RET
viewCallback:
	MOVD ctrl+56(FP), R10
	MOVD 64(RSP), R11
	CMP R10, R11
	BNE viewAbortAdmission
	MOVD const_hcImportIdx(R10), R11
	MOVWU slots+64(FP), R12
	LSL $32, R12, R12
	CMP R12, R11
	BNE viewAbortAdmission
	MOVD trap+32(FP), R9
	MOVWU 0(R9), R11
	CBNZ R11, viewAbortTrap
	MOVWU slots+64(FP), R9
	LSR $16, R9, R4
	ADD $const_hcResults, R10, R3
	MOVD R4, R5
	MOVD R3, R11
	MOVD R4, R12
	CBZ R12, viewResultsCleared
viewClearResults:
	MOVD.P ZR, 8(R11)
	SUB $1, R12, R12
	CBNZ R12, viewClearResults
viewResultsCleared:
	AND $65535, R9, R1
	MOVD R1, R2
	ADD $const_hcArgs, R10, R0
	MOVD fn+0(FP), R26
	MOVD 0(R26), R9
	BL (R9)
	MOVD trap+32(FP), R9
	MOVWU 0(R9), R11
	CBNZ R11, viewAbortTrap
	MOVD ctrl+56(FP), R10
	MOVD $·inlineHostViewStagedBridge(SB), R9
	MOVD R9, const_hcTrampoline(R10)
	MOVD 72(RSP), R9
	MOVD 64(RSP), R11
	JMP ·inlineHostResumeForeign(SB)
viewAbortAdmission:
	MOVD $1, R9
	MOVD R9, ret+72(FP)
	RET
viewAbortTrap:
	MOVD $2, R9
	MOVD R9, ret+72(FP)
	RET

TEXT ·inlineHostViewStagedBridge(SB), NOSPLIT|NOFRAME, $0-0
	MOVD -const_offCustomCtx(R26), R9
	MOVD RSP, R10
	STP (R10, R19), const_hcSavedSP(R9)
	STP (R20, R21), const_hcSavedX20(R9)
	STP (R22, R23), const_hcSavedX22(R9)
	STP (R24, R25), const_hcSavedX24(R9)
	STP (R26, R27), const_hcSavedX26(R9)
	STP (R26, R29), const_hcSavedLinMem(R9)
	MOVD R30, const_hcSavedLR(R9)
	FSTPD (F8, F9), const_hcSavedV8(R9)
	FSTPD (F10, F11), const_hcSavedV10(R9)
	FSTPD (F12, F13), const_hcSavedV12(R9)
	FSTPD (F14, F15), const_hcSavedV14(R9)
	MOVD -const_inlineHostTrapCellOffset(R26), R10
	MOVD R9, 8(R10)
	MOVD R9, R11                 // parked control frame
	MOVD -24(R26), R9            // this entry's foreign landing record
	MOVD 0(R9), R12              // live Go owner SP
	MOVD R11, 64(R12)
	MOVD R9, 72(R12)
	MOVD 88(R9), R29
	MOVD 72(R9), R9
	MOVD R12, RSP
	JMP (R9)

TEXT ·inlineHostViewBridgeAddr(SB), NOSPLIT|NOFRAME, $0-8
	MOVD $·inlineHostViewStagedBridge(SB), R0
	MOVD R0, ret+0(FP)
	RET


// Static typed callback target with an explicit rooted Go context.
TEXT ·inlineHostContextEnter(SB), NOSPLIT, $64-88
	NO_LOCAL_POINTERS
	MOVD ZR, ret+80(FP)
	MOVD code+16(FP), R9
	MOVD args+24(FP), R0
	MOVD linMem+32(FP), R1
	MOVD trap+40(FP), R2
	MOVD results+48(FP), R3
	MOVD stack+56(FP), R10
	ADR context_goCallback, R12
	BL ·inlineHostEnterForeign(SB)
	RET
context_goCallback:
	MOVD ctrl+64(FP), R10
	MOVD 32(RSP), R11
	CMP R10, R11
	BNE context_abortAdmission
	MOVD const_hcImportIdx(R10), R11 // import index and raw slots
	MOVWU slots+72(FP), R12
	LSL $32, R12, R12
	CMP R12, R11
	BNE context_abortAdmission
	MOVD trap+40(FP), R9
	MOVWU 0(R9), R11
	CBNZ R11, context_abortTrap
	MOVD fn+0(FP), R26
	MOVD 0(R26), R9
	MOVD const_hcArgs(R10), R1
	MOVD const_hcArgs+8(R10), R2
	MOVD context+8(FP), R0
	BL (R9)
	MOVD trap+40(FP), R9
	MOVWU 0(R9), R11
	CBNZ R11, context_abortTrap
	MOVD ctrl+64(FP), R10
	MOVWU slots+72(FP), R11
	LSR $16, R11, R11
	CBZ R11, context_resultDone
	CMP $1, R11
	BNE context_twoResults
	MOVD R0, const_hcResults(R10)
	B context_resultDone
context_twoResults:
	MOVWU R0, R12
	LSR $32, R0, R13
	MOVD R12, const_hcResults(R10)
	MOVD R13, const_hcResults+8(R10)
context_resultDone:
	MOVD $·inlineHostStagedBridge(SB), R9
	MOVD R9, const_hcTrampoline(R10)
	MOVD 40(RSP), R9
	MOVD 32(RSP), R11
	JMP ·inlineHostResumeForeign(SB)
context_abortAdmission:
	MOVD $1, R9
	MOVD R9, ret+80(FP)
	RET
context_abortTrap:
	MOVD $2, R9
	MOVD R9, ret+80(FP)
	// Return directly through the normal Go owner epilogue. No foreign
	// continuation executes after cancellation or an admission violation.
	RET

// Static target with a rooted Go context; seven outgoing argument words.
TEXT ·inlineHostViewContextEnter(SB), NOSPLIT, $96-88
	NO_LOCAL_POINTERS
	MOVD ZR, ret+80(FP)
	MOVD code+16(FP), R9
	MOVD args+24(FP), R0
	MOVD linMem+32(FP), R1
	MOVD trap+40(FP), R2
	MOVD results+48(FP), R3
	MOVD stack+56(FP), R10
	ADR context_viewCallback, R12
	BL ·inlineHostEnterForeign(SB)
	RET
context_viewCallback:
	MOVD ctrl+64(FP), R10
	MOVD 64(RSP), R11
	CMP R10, R11
	BNE context_viewAbortAdmission
	MOVD const_hcImportIdx(R10), R11
	MOVWU slots+72(FP), R12
	LSL $32, R12, R12
	CMP R12, R11
	BNE context_viewAbortAdmission
	MOVD trap+40(FP), R9
	MOVWU 0(R9), R11
	CBNZ R11, context_viewAbortTrap
	MOVWU slots+72(FP), R9
	LSR $16, R9, R5
	ADD $const_hcResults, R10, R4
	MOVD R5, R6
	MOVD R4, R11
	MOVD R5, R12
	CBZ R12, context_viewResultsCleared
context_viewClearResults:
	MOVD.P ZR, 8(R11)
	SUB $1, R12, R12
	CBNZ R12, context_viewClearResults
context_viewResultsCleared:
	AND $65535, R9, R2
	MOVD R2, R3
	ADD $const_hcArgs, R10, R1
	MOVD context+8(FP), R0
	MOVD fn+0(FP), R26
	MOVD 0(R26), R9
	BL (R9)
	MOVD trap+40(FP), R9
	MOVWU 0(R9), R11
	CBNZ R11, context_viewAbortTrap
	MOVD ctrl+64(FP), R10
	MOVD $·inlineHostViewStagedBridge(SB), R9
	MOVD R9, const_hcTrampoline(R10)
	MOVD 72(RSP), R9
	MOVD 64(RSP), R11
	JMP ·inlineHostResumeForeign(SB)
context_viewAbortAdmission:
	MOVD $1, R9
	MOVD R9, ret+80(FP)
	RET
context_viewAbortTrap:
	MOVD $2, R9
	MOVD R9, ret+80(FP)
	RET
