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
	MOVD	16(R23), R9             // linMem
	MOVD	216(R22), R1            // CONTEXT.X[26]
	CMP	R1, R9
	BNE	next
	MOVD	R21, R1
	SUB	R9, R1
	MOVWU	-8(R9), R2
	CMP	R2, R1
	BHS	outofbounds

	MOVD	R1, R0
	AND	$-65536, R0
	ADD	R9, R0
	SUB	$16, RSP
	MOVD	R0, 0(RSP)
	MOVD	$65536, R1
	MOVD	R1, 8(RSP)
	MOVD	$-1, R0                 // current process
	MOVD	RSP, R1                 // in/out base address
	MOVD	$0, R2                  // ZeroBits
	ADD	$8, RSP, R3             // in/out region size
	MOVD	$0x1000, R4             // MEM_COMMIT
	MOVD	$4, R5                  // PAGE_READWRITE
	MOVD	·guardNtAllocateVirtualMemoryPC(SB), R16
	BL	(R16)
	ADD	$16, RSP
	CBZ	R0, continued            // NTSTATUS_SUCCESS
	MOVW	$4, R2
	B	settrap
outofbounds:
	MOVW	$3, R2
settrap:
	MOVD	-104(R9), R1
	CBZ	R1, search
	MOVW	R2, (R1)
	MOVD	R9, 80(R22)             // CONTEXT.X[9] for landing pad
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
