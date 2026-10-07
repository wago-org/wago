//go:build !tinygo

#include "textflag.h"

TEXT ·readTestMXCSR(SB), NOSPLIT, $0-4
	STMXCSR ret+0(FP)
	RET

TEXT ·writeTestMXCSR(SB), NOSPLIT, $0-4
	LDMXCSR value+0(FP)
	RET
