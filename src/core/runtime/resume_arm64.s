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

	// Save only Go's callee-saved GP state (see trampoline_arm64.s: Go's arm64 ABI
	// keeps no callee-saved V registers, so V8-V15 need not be preserved for the
	// Go side). The wasm activation's own V8-V15 are restored from the control
	// frame in resumeWasm below.
	SUB  $176, R10, R10
	MOVD RSP, R11
	MOVD R11, 0(R10)
	STP (R19, R20), 8(R10)
	STP (R21, R22), 24(R10)
	STP (R23, R24), 40(R10)
	STP (R25, R26), 56(R10)
	STP (R27, g), 72(R10)
	STP (R29, R30), 88(R10)

	MOVD 80(R9), R26               // X26 = saved linMem
	MOVD R10, -24(R26)
	BL   resumeWasm

afterResume:
	LDP 8(RSP), (R19, R20)
	LDP 24(RSP), (R21, R22)
	LDP 40(RSP), (R23, R24)
	LDP 56(RSP), (R25, R26)
	LDP 72(RSP), (R27, g)
	LDP 88(RSP), (R29, R30)
	MOVD 0(RSP), R11
	MOVD R11, RSP
	RET

resumeWasm:
	MOVD R30, R11                  // afterResume continuation PC
	// Store the continuation PC at the trap-handler slot (arm64TrapHandlerPtrOffset
	// = 32), matching enterNative and the backend's offTrapHandlerPtr. Offset 16 is
	// WRONG here: a u64 there overlaps the max-pages cache at -12, so the PC's high
	// word clobbers the memory.grow ceiling — every host call would then break the
	// next memory.grow (and thus heap allocation).
	MOVD R11, -32(R26)
	LDP 8(R9), (R19, R20)
	LDP 24(R9), (R21, R22)
	LDP 40(R9), (R23, R24)
	LDP 56(R9), (R25, R26)
	MOVD 72(R9), R27
	// X28 is Go's g register on arm64. Native code keeps it intact; linMem lives
	// in X26, restored above and again from the callee-saved block.
	LDP 88(R9), (R29, R30)
	FLDPD 104(R9), (F8, F9)
	FLDPD 120(R9), (F10, F11)
	FLDPD 136(R9), (F12, F13)
	FLDPD 152(R9), (F14, F15)
	MOVD 0(R9), R11
	MOVD R11, RSP
	JMP  (R30)
