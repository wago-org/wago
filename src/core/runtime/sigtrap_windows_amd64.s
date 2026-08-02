//go:build windows && amd64 && wago_guardpage

#include "textflag.h"

TEXT ·nativeTrapExitHandlerJump(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	-24(BX), SP
	RET

TEXT ·addrNativeTrapExitHandlerJump(SB), NOSPLIT, $0-8
	LEAQ	·nativeTrapExitHandlerJump(SB), AX
	MOVQ	AX, ret+0(FP)
	RET
