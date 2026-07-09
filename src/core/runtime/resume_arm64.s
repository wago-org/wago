//go:build (linux || darwin) && arm64 && !tinygo

#include "textflag.h"

// func resumeNative(ctrl, foreignStackTop uintptr)
//
// Restores a wasm activation parked by hostCallStub. It first records the
// current Go context in the foreign-stack save area, then restores the wasm
// callee-saved state, SP, and LR from the control frame and RETs to the wasm
// resume address. When the resumed wasm finishes or parks again it jumps to this
// function's epilogue through the basedata re-entry cells.
TEXT ·resumeNative(SB), NOSPLIT, $0-16
	MOVD ctrl+0(FP), R9
	MOVD foreignStackTop+8(FP), R10

	SUB  $176, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	MOVD R19, 8(R10)
	MOVD R20, 16(R10)
	MOVD R21, 24(R10)
	MOVD R22, 32(R10)
	MOVD R23, 40(R10)
	MOVD R24, 48(R10)
	MOVD R25, 56(R10)
	MOVD R26, 64(R10)
	MOVD R27, 72(R10)
	MOVD g, 80(R10)
	MOVD R29, 88(R10)
	MOVD R30, 96(R10)
	FMOVD F8, 104(R10)
	FMOVD F9, 112(R10)
	FMOVD F10, 120(R10)
	FMOVD F11, 128(R10)
	FMOVD F12, 136(R10)
	FMOVD F13, 144(R10)
	FMOVD F14, 152(R10)
	FMOVD F15, 160(R10)

	MOVD 80(R9), R26               // X26 = saved linMem
	MOVD R10, -24(R26)
	BL   resumeWasm

afterResume:
	FMOVD 104(RSP), F8
	FMOVD 112(RSP), F9
	FMOVD 120(RSP), F10
	FMOVD 128(RSP), F11
	FMOVD 136(RSP), F12
	FMOVD 144(RSP), F13
	FMOVD 152(RSP), F14
	FMOVD 160(RSP), F15
	MOVD 8(RSP), R19
	MOVD 16(RSP), R20
	MOVD 24(RSP), R21
	MOVD 32(RSP), R22
	MOVD 40(RSP), R23
	MOVD 48(RSP), R24
	MOVD 56(RSP), R25
	MOVD 64(RSP), R26
	MOVD 72(RSP), R27
	MOVD 80(RSP), g
	MOVD 88(RSP), R29
	MOVD 96(RSP), R30
	MOVD 0(RSP), R11
	MOVD R11, RSP
	RET

resumeWasm:
	MOVD R30, R11                  // afterResume continuation PC
	MOVD R11, -16(R26)
	MOVD 8(R9), R19
	MOVD 16(R9), R20
	MOVD 24(R9), R21
	MOVD 32(R9), R22
	MOVD 40(R9), R23
	MOVD 48(R9), R24
	MOVD 56(R9), R25
	MOVD 64(R9), R26
	MOVD 72(R9), R27
	// X28 is Go's g register on arm64. Native code keeps it intact; linMem lives
	// in X26, restored above and again from the callee-saved block.
	MOVD 88(R9), R29
	MOVD 96(R9), R30
	FMOVD 104(R9), F8
	FMOVD 112(R9), F9
	FMOVD 120(R9), F10
	FMOVD 128(R9), F11
	FMOVD 136(R9), F12
	FMOVD 144(R9), F13
	FMOVD 152(R9), F14
	FMOVD 160(R9), F15
	MOVD 0(R9), R11
	MOVD R11, RSP
	JMP  (R30)
