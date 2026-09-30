//go:build windows && arm64 && wago_guardpage

#include "textflag.h"

// Windows arm64 VEH ABI: R0=*EXCEPTION_POINTERS.
TEXT ·guardExceptionHandler(SB), NOSPLIT|NOFRAME, $0-0
	SUB	$64, RSP
	STP	(R19, R20), 0(RSP)
	STP	(R21, R22), 16(RSP)
	STP	(R23, R24), 32(RSP)
	STP	(R29, R30), 48(RSP)
	MOVD	R0, R19
	MOVD	0(R0), R20              // EXCEPTION_RECORD
	MOVWU	0(R20), R1
	MOVD	$0xc0000005, R2
	CMPW	R2, R1
	BNE	search
	MOVWU	24(R20), R1
	CMPW	$2, R1
	BLO	search
	MOVD	40(R20), R21            // fault address
	MOVD	8(R19), R22             // CONTEXT
	MOVD	$·guardRegions(SB), R23
	MOVD	$256, R24
scan:
	LDAR	(R23), R1
	CBZ	R1, next
	CMP	R1, R21
	BLO	next
	MOVD	8(R23), R1
	CMP	R1, R21
	BHS	next
	MOVD	16(R23), R9             // region.linMem (fault-address base)
	MOVD	216(R22), R1            // CONTEXT.X[26] primary linMem
	MOVD	24(R23), R2             // region.ownerLinMem
	CMP	R1, R2
	BNE	next
	MOVD	R21, R1
	SUB	R9, R1
	MOVD	-288(R9), R2
	CMP	R2, R1
	BHS	outofbounds

	AND	$-65536, R1
	ADD	R9, R1                  // allocation-aligned page
	MOVD	256(R22), R2            // saved SP
	SUB	$16, R2
	MOVD	R1, 0(R2)
	MOVD	264(R22), R1            // faulting PC
	MOVD	R1, 8(R2)
	MOVD	R2, 256(R22)
	MOVD	$·guardCommitPage(SB), R1
	MOVD	R1, 264(R22)
	B	continued
outofbounds:
	MOVW	$3, R2
settrap:
	MOVD	216(R22), R1            // active primary linMem
	MOVD	-104(R1), R3
	CBZ	R3, search
	MOVW	R2, (R3)
	MOVD	R1, 80(R22)             // CONTEXT.X[9] for landing pad
	MOVD	·guardTrapExitHandlerJumpPC(SB), R1
	MOVD	R1, 264(R22)            // CONTEXT.Pc
continued:
	MOVD	$-1, R0
	B	done
next:
	ADD	$32, R23
	SUB	$1, R24
	CBNZ	R24, scan
search:
	MOVD	$0, R0
done:
	LDP	0(RSP), (R19, R20)
	LDP	16(RSP), (R21, R22)
	LDP	32(RSP), (R23, R24)
	LDP	48(RSP), (R29, R30)
	ADD	$64, RSP
	RET

// Entry SP points at {page, retryPC}. Preserve all allocator-visible volatile
// GP/vector state around VirtualAlloc, then branch back to the faulting PC. X17
// is the backend's dedicated logical-PC scratch and carries the retry target;
// X16 must be restored because memory lowering uses it as an address scratch.
TEXT ·guardCommitPage(SB), NOSPLIT|NOFRAME, $0-0
	SUB	$672, RSP
	STP	(R0, R1), 0(RSP)
	STP	(R2, R3), 16(RSP)
	STP	(R4, R5), 32(RSP)
	STP	(R6, R7), 48(RSP)
	STP	(R8, R9), 64(RSP)
	STP	(R10, R11), 80(RSP)
	STP	(R12, R13), 96(RSP)
	STP	(R14, R15), 112(RSP)
	STP	(R16, R17), 128(RSP)
	FSTPQ	(F0, F1), 144(RSP)
	FSTPQ	(F2, F3), 176(RSP)
	FSTPQ	(F4, F5), 208(RSP)
	FSTPQ	(F6, F7), 240(RSP)
	FSTPQ	(F8, F9), 272(RSP)
	FSTPQ	(F10, F11), 304(RSP)
	FSTPQ	(F12, F13), 336(RSP)
	FSTPQ	(F14, F15), 368(RSP)
	FSTPQ	(F16, F17), 400(RSP)
	FSTPQ	(F18, F19), 432(RSP)
	FSTPQ	(F20, F21), 464(RSP)
	FSTPQ	(F22, F23), 496(RSP)
	FSTPQ	(F24, F25), 528(RSP)
	FSTPQ	(F26, F27), 560(RSP)
	FSTPQ	(F28, F29), 592(RSP)
	FSTPQ	(F30, F31), 624(RSP)
	MRS	NZCV, R17
	MOVD	R17, 656(RSP)
	MOVD	R30, 664(RSP)          // interrupted leaf return address
	MOVD	672(RSP), R0           // allocation-aligned page from VEH frame
	MOVD	$65536, R1
	MOVD	$0x1000, R2            // MEM_COMMIT
	MOVD	$4, R3                 // PAGE_READWRITE
	MOVD	·guardVirtualAllocPC(SB), R16
	BL	(R16)
	CBZ	R0, commitfailed
	FLDPQ	144(RSP), (F0, F1)
	FLDPQ	176(RSP), (F2, F3)
	FLDPQ	208(RSP), (F4, F5)
	FLDPQ	240(RSP), (F6, F7)
	FLDPQ	272(RSP), (F8, F9)
	FLDPQ	304(RSP), (F10, F11)
	FLDPQ	336(RSP), (F12, F13)
	FLDPQ	368(RSP), (F14, F15)
	FLDPQ	400(RSP), (F16, F17)
	FLDPQ	432(RSP), (F18, F19)
	FLDPQ	464(RSP), (F20, F21)
	FLDPQ	496(RSP), (F22, F23)
	FLDPQ	528(RSP), (F24, F25)
	FLDPQ	560(RSP), (F26, F27)
	FLDPQ	592(RSP), (F28, F29)
	FLDPQ	624(RSP), (F30, F31)
	MOVD	656(RSP), R17
	MSR	R17, NZCV
	LDP	0(RSP), (R0, R1)
	LDP	16(RSP), (R2, R3)
	LDP	32(RSP), (R4, R5)
	LDP	48(RSP), (R6, R7)
	LDP	64(RSP), (R8, R9)
	LDP	80(RSP), (R10, R11)
	LDP	96(RSP), (R12, R13)
	LDP	112(RSP), (R14, R15)
	MOVD	128(RSP), R16          // restore the memory-lowering scratch
	MOVD	664(RSP), R30          // VirtualAlloc overwrites LR
	MOVD	680(RSP), R17          // retry PC in the dedicated logical-PC scratch
	ADD	$688, RSP
	B	(R17)
commitfailed:
	MOVD	-104(R26), R1
	CBZ	R1, commitsearch
	MOVW	$4, R2                 // TrapLinMemCouldNotExtend
	MOVW	R2, (R1)
	MOVD	R26, R9                // landing pad requires primary linMem in X9
	B	·nativeTrapExitHandlerJump(SB)
commitsearch:
	BRK	$0

TEXT ·addrGuardExceptionHandler(SB), NOSPLIT, $0-8
	MOVD	$·guardExceptionHandler(SB), R0
	MOVD	R0, ret+0(FP)
	RET

TEXT ·nativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	-24(R9), R10
	MOVD	R10, RSP
	MOVD	-32(R9), R10
	B	(R10)

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT, $0-8
	MOVD	$·nativeTrapExitHandlerJump(SB), R0
	MOVD	R0, ret+0(FP)
	RET
