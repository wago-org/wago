//go:build wago_inline_host_experiment && (linux || darwin) && arm64 && !tinygo

#include "textflag.h"
#include "funcdata.h"

// The 64-byte Go locals remain on the Go stack throughout foreign execution.
// Outgoing values are scalar. fn is retained in the generated argument map.
TEXT ·inlineHostProbe(SB), NOSPLIT, $64-48
	NO_LOCAL_POINTERS
	MOVD code+8(FP), R9
	MOVD args+16(FP), R0
	MOVD linMem+24(FP), R1
	MOVD results+32(FP), R3
	MOVD stack+40(FP), R10
	ADR goCallback, R12
	BL ·inlineHostProbeEnter(SB)
	RET

goCallback:
	// fn is the ABIInternal funcval; its code uses R0 and its closure R26.
	// Only the verified one-integer/one-integer Go register ABI is probed.
	MOVD fn+0(FP), R26
	MOVD 0(R26), R9
	MOVD 16(RSP), R0
	BL (R9)
	MOVD 40(RSP), R9
	MOVD 32(RSP), R11
	JMP ·inlineHostProbeResumeForeign(SB)

// SP writes are confined to non-scannable foreign transition functions. The
// live Go owner above has ordinary stack metadata and remains copyable.
TEXT ·inlineHostProbeEnter(SB), NOSPLIT|NOFRAME, $0-0
	SUB $176, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	MOVD R26, 64(R10)
	MOVD R12, 72(R10)
	STP (R29, R30), 88(R10)
	MOVD R10, -24(R1)
	MOVD R10, RSP
	BL (R9)
	MOVD 64(RSP), R26
	LDP 88(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
	JMP (R30)

TEXT ·inlineHostProbeResumeForeign(SB), NOSPLIT|NOFRAME, $0-0
	MOVD RSP, R12
	MOVD R12, 0(R9)                 // stack growth may have moved the Go frame
	MOVD R29, 88(R9)                // preserve the current Go owner frame pointer
	MOVD R11, RSP
	LDP 0(RSP), (R19, R20)
	LDP 16(RSP), (R21, R22)
	LDP 32(RSP), (R23, R24)
	LDP 48(RSP), (R25, R26)
	MOVD 64(RSP), R27
	LDP 80(RSP), (R29, R30)
	FLDPD 96(RSP), (F8, F9)
	FLDPD 112(RSP), (F10, F11)
	FLDPD 128(RSP), (F12, F13)
	FLDPD 144(RSP), (F14, F15)
	ADD $176, RSP, RSP
	JMP (R30)

// Native entry: no Go frame exists here. Save the native caller on its own
// stack, then jump into the live Go frame above before invoking Go code.
TEXT ·inlineHostProbeBridge(SB), NOSPLIT|NOFRAME, $0-0
	SUB $176, RSP, RSP
	STP (R19, R20), 0(RSP)
	STP (R21, R22), 16(RSP)
	STP (R23, R24), 32(RSP)
	STP (R25, R26), 48(RSP)
	MOVD R27, 64(RSP)
	STP (R29, R30), 80(RSP)
	FSTPD (F8, F9), 96(RSP)
	FSTPD (F10, F11), 112(RSP)
	FSTPD (F12, F13), 128(RSP)
	FSTPD (F14, F15), 144(RSP)
	MOVD -24(R26), R9
	MOVD 0(R9), R12
	MOVD RSP, R11
	MOVD R11, 32(R12)
	MOVD R9, 40(R12)
	MOVD R0, 16(R12)
	MOVD 88(R9), R29
	MOVD 72(R9), R9
	MOVD R12, RSP
	JMP (R9)

TEXT ·inlineHostProbeBridgeAddr(SB), NOSPLIT|NOFRAME, $0-8
	MOVD $·inlineHostProbeBridge(SB), R0
	MOVD R0, ret+0(FP)
	RET

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
