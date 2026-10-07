//go:build wago_inline_host_experiment && (linux || darwin) && arm64 && !tinygo

#include "textflag.h"

// Test-only Go ABI helper: Go has no callee-saved floating register bank.
// Deliberately overwrite the native callee-saved bank from a real Go callback.
TEXT ·inlineHostProbeClobberFP(SB), NOSPLIT|NOFRAME, $0-0
	MOVD ZR, R0
	FMOVD R0, F8
	FMOVD R0, F9
	FMOVD R0, F10
	FMOVD R0, F11
	FMOVD R0, F12
	FMOVD R0, F13
	FMOVD R0, F14
	FMOVD R0, F15
	RET
