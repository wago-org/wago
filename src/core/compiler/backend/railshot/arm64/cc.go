//go:build arm64

// Package railshot is wago's single-pass AArch64 (arm64) code generator — the
// "railshot" compiler tier (it lives under backend/railshot; a future optimizing
// tier will sit alongside it). Ported from the WARP engine's single-pass compiler
// (warp/, Apache-2.0), it reimplements WARP's
// architecture in Go: a valent-block / deferred-action operand model with an
// on-the-fly whole-register-file register allocator (the "condense engine"),
// which is what lets WARP keep locals and temporaries resident and spill only
// under pressure — the property wago's original single-pass backend lacked.
//
// This package reuses wago's existing pieces: the wasm decoder/validator
// (src/core/compiler/wasm), the golden-tested AArch64 instruction encoders
// (src/core/encoder/arm64.Asm), and the runtime (engine/MapCode/JobMemory/trampoline).
// It targets wago's runtime ABI, not WARP's binary format.
//
// Derived from WARP (github: the warp/ submodule), Apache-2.0.
package arm64

import a64 "github.com/wago-org/wago/src/core/encoder/arm64"

// Reg and the register constants are reused from the arm64 encoder package so
// this backend can drive a64.Asm directly.
type Reg = a64.Reg

// Cond is the arm64 condition code, reused for compare/cset lowering.
type Cond = a64.Cond

// AArch64 condition codes, named after the amd64 backend's condXX constants so
// the compare/branch code is a mechanical rename. AArch64 has no parity flag, so
// condP/condNP are intentionally absent — the float compare path lowers unordered
// cases via FCMP's defined NZCV result (see fp.go), not a parity sequence.
const (
	condE  = a64.CondEQ // ==
	condNE = a64.CondNE // !=
	condB  = a64.CondCC // unsigned <  (LO)
	condAE = a64.CondCS // unsigned >= (HS)
	condBE = a64.CondLS // unsigned <=
	condA  = a64.CondHI // unsigned >
	condL  = a64.CondLT // signed <
	condGE = a64.CondGE // signed >=
	condLE = a64.CondLE // signed <=
	condG  = a64.CondGT // signed >
	condS  = a64.CondMI // negative (sign)
)

const (
	X0        = a64.X0
	X1        = a64.X1
	X2        = a64.X2
	X3        = a64.X3
	X4        = a64.X4
	X5        = a64.X5
	X6        = a64.X6
	X7        = a64.X7
	X8        = a64.X8
	X9        = a64.X9
	X10       = a64.X10
	X11       = a64.X11
	X12       = a64.X12
	X13       = a64.X13
	X14       = a64.X14
	X15       = a64.X15
	X16       = a64.X16
	X17       = a64.X17
	X18       = a64.X18
	X19       = a64.X19
	X20       = a64.X20
	X21       = a64.X21
	X22       = a64.X22
	X23       = a64.X23
	X24       = a64.X24
	X25       = a64.X25
	X26       = a64.X26
	X27       = a64.X27
	X28       = a64.X28
	linMemReg = X26
	FP        = a64.FP // X29, frame pointer
	LR        = a64.LR // X30, link register
	SP        = a64.SP // 31 in add/sub-imm and load/store base position
	ZR        = a64.ZR // 31, zero register in most encodings
)

// Register roles, mirroring WARP's WasmABI::REGS (aarch64_cc.hpp) but adapted to
// wago's runtime. wago's enterNative passes linMem in X1 (the wrapper-ABI arg);
// the function prologue moves it into linMemReg and keeps it there for the whole
// function. WARP uses X28 for this role, but Go uses X28 as the arm64 g register;
// keeping g intact while native code runs avoids async-preemption/signal crashes.
// X26 is AAPCS64 callee-saved and enterNative saves/restores it, so the runtime
// boundary is unaffected.

// gpAlloc is the general-purpose register allocation pool, in priority order.
// Mirrors WARP's `gpr` array (aarch64_cc.hpp):
//   - The caller-saved temporaries (X9-X15, then the remaining arg regs) come
//     first: they are the preferred homes for short-lived values, and they are
//     spilled around calls like any other operand register.
//   - The callee-saved block (X19-X27) follows: these are the pinned-local /
//     module-global / memSize candidates, carved out per module via f.reserved
//     (exactly as amd64 keeps R15/R12-R14 in gpAlloc and removes them dynamically).
//   - X1/X0 (the AAPCS64 arg/result registers) come last, so general values avoid
//     the result register until forced.
//
// Permanently excluded (never allocatable): linMemReg/X26 (linMem), X16/X17
// (backend scratch, IP0/IP1), X18 (platform), X28 (Go g), X29/FP, X30/LR, 31
// (SP/ZR).
//
// The last numScratchGP entries are the reserved scratch registers. Unlike x86,
// AArch64 has no fixed-register ALU ops (mul/div/shift are all orthogonal), so the
// reserved tail exists only to keep the result register free; it is allocated last
// for general values.
var gpAlloc = []Reg{
	// freely allocatable, caller-saved temporaries (preferred for short-lived ops)
	X9, X10, X11, X12, X13, X14, X15, X8, X7, X6, X5, X4, X3, X2,
	// callee-saved: pinned-local / module-global / memSize candidates
	X19, X20, X21, X22, X23, X24, X25, X27,
	// reserved scratch / result, allocated last (mirrors amd64 RAX/RDX/RCX/R8)
	X1, X0,
}

// numScratchGP is how many trailing gpAlloc entries are reserved scratch, matching
// WARP's resScratchRegsGPR. These are preferred last for holding locals/values.
// On arm64 there are no fixed-register instructions, so this shrinks to 2 (X1, X0)
// versus amd64's 4 — the tail only reserves the result register.
const numScratchGP = 2

// pinnedLocalRegs are callee-saved registers dedicated to hot integer locals
// (WARP recoverLocalToReg). enterNative preserves them across the Go boundary;
// wasm callees clobber them, so callers spill/reload pinned locals around calls.
// linMemReg is excluded (linMem); X24/X25 and X27 are also callee-saved but
// reserved for the linMem-size / module-global roles, so the base pin pool uses
// X19-X23.
var pinnedLocalRegs = []Reg{X19, X20, X21, X22, X23}

// pinnedFLocalRegs are V registers dedicated to hot float locals. V8-V15 are the
// AAPCS64 callee-saved FP range (only the low 64 bits are preserved), so like the
// GP pinned locals the trampoline saves/restores them across the Go boundary; V0-V7
// and V16+ stay in the operand pool. V8-V14 are available for local pins; V15 is
// reserved as mergeFReg for single-result float control-flow joins.
var pinnedFLocalRegs = []Reg{8, 9, 10, 11, 12, 13, 14}

// fpAllocRegs is the transient FP/SIMD operand pool. Keep the historical V0-V15
// order first for stable codegen, then use the caller-saved V16-V31 range before
// spilling under high vector pressure.
var fpAllocRegs = []Reg{
	0, 1, 2, 3, 4, 5, 6, 7,
	8, 9, 10, 11, 12, 13, 14, 15,
	16, 17, 18, 19, 20, 21, 22, 23,
	24, 25, 26, 27, 28, 29, 30, 31,
}

// isScratchGP reports whether r is one of the reserved scratch GPRs (the trailing
// numScratchGP of gpAlloc).
func isScratchGP(r Reg) bool {
	for _, s := range gpAlloc[len(gpAlloc)-numScratchGP:] {
		if s == r {
			return true
		}
	}
	return false
}

// gpAllocPos returns r's index in gpAlloc, or -1 if r is not allocatable
// (linMemReg, X16/X17, X18, X28, FP, LR, SP). Lower index = higher allocation
// priority.
func gpAllocPos(r Reg) int {
	for i, a := range gpAlloc {
		if a == r {
			return i
		}
	}
	return -1
}
