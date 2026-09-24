//go:build arm64 && !tinygo

#include "textflag.h"

// copyNarrowScalarSlotsSIMD clears the high 32 bits of each 64-bit slot.
// NEON operates on two slots per vector; the final odd slot is scalar.
TEXT ·copyNarrowScalarSlotsSIMD(SB), NOSPLIT, $0-24
	MOVD dst+0(FP), R0
	MOVD src+8(FP), R1
	MOVD n+16(FP), R2
	MOVD $0xffffffff, R3
	VMOV R3, V1.D2
	CMP $2, R2
	BLT tail
loop:
	VLD1.P 16(R1), [V0.B16]
	VAND V1.B16, V0.B16, V0.B16
	VST1.P [V0.B16], 16(R0)
	SUB $2, R2, R2
	CMP $2, R2
	BGE loop
tail:
	CBZ R2, done
	MOVWU (R1), R3
	MOVD R3, (R0)
done:
	RET
