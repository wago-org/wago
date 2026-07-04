//go:build !tinygo

#include "textflag.h"

// func resumeNative(ctrl, foreignStackTop uintptr)
//
// Resumes native wasm code parked at a returning host import by hostCallStub.
// hostCallStub saved the wasm callee-saved registers + RSP into the control
// frame and unwound to Go through the trap re-entry SP; resumeNative reverses
// that. It re-stashes the *current* Go context into the same save area at
// foreignStackTop-64 that enterNative uses, so when the resumed wasm eventually
// unwinds (completion, trap, or another host call) it returns through
// enterNative's shared epilogue — reached via the return address still parked at
// foreignStackTop-72 — which restores this Go context and returns here. Then it
// reloads the saved wasm registers + RSP and RETs into wasm at the instruction
// after the host CALL. See docs/host-import-results-plan.md §2.
//
// The deep wasm frames below the save area are untouched while Go runs (Go
// executes on the goroutine stack), so the parked stack is intact on resume.
TEXT ·resumeNative(SB), NOSPLIT, $0-16
	MOVQ ctrl+0(FP), R9            // read args before altering SP (FP still valid)
	MOVQ foreignStackTop+8(FP), R10

	// enterNative is non-leaf, so the linker frames it with PUSHQ BP / ... /
	// POPQ BP; RET, and the goroutine SP it stashes points at that pushed BP with
	// the return address just above. resumeNative reuses enterNative's epilogue
	// (via the return address parked at foreignStackTop-72), so it must stash an
	// SP of the SAME shape — otherwise the epilogue's POPQ BP eats the return
	// address and RET jumps into the control frame. Emulate `PUSHQ BP; MOVQ SP,BP`
	// with SUBQ/MOVQ so the assembler's PUSH/POP balance check (resumeNative never
	// pops — it RETs into wasm) does not reject it.
	SUBQ $8, SP
	MOVQ BP, 0(SP)                 // [SP] = caller BP; return address now at 8(SP)
	MOVQ SP, BP

	SUBQ $64, R10                  // save-area base (identical to enterNative)
	MOVQ SP,  0(R10)               // goroutine SP, now pointing at the pushed BP
	MOVQ BP,  8(R10)
	MOVQ BX, 16(R10)
	MOVQ R12, 24(R10)
	MOVQ R13, 32(R10)
	MOVQ R14, 40(R10)              // g
	MOVQ R15, 48(R10)

	// Reload the wasm register state hostCallStub saved into the control frame.
	MOVQ  8(R9), BX                // hcSavedRBX (linMem)
	MOVQ 16(R9), BP                // hcSavedRBP
	MOVQ 24(R9), R12               // hcSavedR12
	MOVQ 32(R9), R13               // hcSavedR13
	MOVQ 40(R9), R14               // hcSavedR14
	MOVQ 48(R9), R15               // hcSavedR15
	MOVQ  0(R9), SP                // hcSavedRSP -> the parked deep wasm stack

	RET                            // pop the wasm resume address, continue in wasm
