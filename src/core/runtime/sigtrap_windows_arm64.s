//go:build windows && arm64 && wago_guardpage

#include "textflag.h"

TEXT ·nativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-0
	MOVD	-24(R9), R10
	MOVD	R10, RSP
	MOVD	-32(R9), R10
	B	(R10)

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT, $0-8
	MOVD	$·nativeTrapExitHandlerJump(SB), R0
	MOVD	R0, ret+0(FP)
	RET
