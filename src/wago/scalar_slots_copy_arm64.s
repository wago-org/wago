//go:build arm64 && !tinygo

#include "textflag.h"

// copyNarrowScalarSlotsSIMD clears the high 32 bits of each 64-bit slot.
// Four NEON vectors cover eight slots per loop; odd tails remain scalar.
TEXT ·copyNarrowScalarSlotsSIMD(SB), NOSPLIT, $0-24
	MOVD dst+0(FP), R0
	MOVD src+8(FP), R1
	MOVD n+16(FP), R2
	MOVD $0xffffffff, R3
	VMOV R3, V5.D2
	CMP $8, R2
	BLT tail2
loop8:
	VLD1.P 64(R1), [V0.B16, V1.B16, V2.B16, V3.B16]
	VAND V5.B16, V0.B16, V0.B16
	VAND V5.B16, V1.B16, V1.B16
	VAND V5.B16, V2.B16, V2.B16
	VAND V5.B16, V3.B16, V3.B16
	VST1.P [V0.B16, V1.B16, V2.B16, V3.B16], 64(R0)
	SUB $8, R2, R2
	CMP $8, R2
	BGE loop8
tail2:
	CMP $2, R2
	BLT tail1
	VLD1.P 16(R1), [V0.B16]
	VAND V5.B16, V0.B16, V0.B16
	VST1.P [V0.B16], 16(R0)
	SUB $2, R2, R2
	B tail2
tail1:
	CBZ R2, done
	MOVWU (R1), R3
	MOVD R3, (R0)
done:
	RET
