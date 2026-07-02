// Package x64 is wago's single-pass x86-64 code generator — the "railshot"
// compiler tier (it lives under backend/railshot; a future optimizing tier will
// sit alongside it). Ported from the WARP engine's single-pass compiler (warp/,
// Apache-2.0), it reimplements WARP's
// architecture in Go: a valent-block / deferred-action operand model with an
// on-the-fly whole-register-file register allocator (the "condense engine"),
// which is what lets WARP keep locals and temporaries resident and spill only
// under pressure — the property wago's original single-pass backend lacked.
//
// This package reuses wago's existing pieces: the wasm decoder/validator
// (src/core/compiler/wasm), the golden-tested x86-64 instruction encoders
// (backend/railshot/amd64.Asm), and the runtime (engine/MapCode/JobMemory/trampoline).
// It targets wago's runtime ABI, not WARP's binary format.
//
// Derived from WARP (github: the warp/ submodule), Apache-2.0.
package x64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"

// Reg and the register constants are reused from the amd64 encoder package so
// this backend can drive amd64.Asm directly.
type Reg = amd64.Reg

// Cond is the amd64 condition code, reused for compare/setcc lowering.
type Cond = amd64.Cond

const (
	condE  = amd64.CondE
	condNE = amd64.CondNE
	condB  = amd64.CondB
	condAE = amd64.CondAE
	condBE = amd64.CondBE
	condA  = amd64.CondA
	condL  = amd64.CondL
	condGE = amd64.CondGE
	condLE = amd64.CondLE
	condG  = amd64.CondG
	condP  = amd64.CondP
	condNP = amd64.CondNP
	condS  = amd64.CondS
)

const (
	RAX = amd64.RAX
	RCX = amd64.RCX
	RDX = amd64.RDX
	RBX = amd64.RBX
	RSP = amd64.RSP
	RBP = amd64.RBP
	RSI = amd64.RSI
	RDI = amd64.RDI
	R8  = amd64.R8
	R9  = amd64.R9
	R10 = amd64.R10
	R11 = amd64.R11
	R12 = amd64.R12
	R13 = amd64.R13
	R14 = amd64.R14
	R15 = amd64.R15
)

// Register roles, mirroring WARP's WasmABI::REGS (x86_64_cc.hpp) but adapted to
// wago's runtime. wago's enterNative passes linMem in RSI; the function prologue
// moves it into RBX and keeps it there for the whole function (WARP's convention:
// linMem lives in RBX). RBX is callee-saved and enterNative saves/restores it, so
// the runtime boundary is unaffected.

// gpAlloc is the general-purpose register allocation pool, in priority order.
// Mirrors WARP's `gpr` array (x86_64_cc.hpp):
//   - RBP IS allocatable: the backend is frameless (locals/spills are RSP-relative),
//     so RBP is a general register just like WARP. As an operand register it is
//     spilled around calls like any other value (not preserved across wasm calls).
//   - RBX is excluded because it is dedicated to linMem (WARP lists it as a REGS
//     constant, not in `gpr`, likewise).
//
// The last numScratchGP entries are the reserved scratch registers: they double
// as the fixed-role registers x86 mandates (RAX/RDX for mul/div, RCX for shifts,
// and the return registers), so they are allocated last for general values.
var gpAlloc = []Reg{
	RDI, RSI, RBP, R9, R10, R11, R12, R13, R14, R15, // freely allocatable
	RAX, RDX, RCX, R8, // reserved scratch (fixed x86 roles)
}

// numScratchGP is how many trailing gpAlloc entries are reserved scratch, matching
// WARP's resScratchRegsGPR. These are preferred last for holding locals/values.
const numScratchGP = 4

// pinnedLocalRegs are callee-saved registers dedicated to hot integer locals
// (WARP recoverLocalToReg). enterNative preserves them across the Go boundary;
// wasm callees clobber them, so callers spill/reload pinned locals around calls.
// RBX is excluded (linMem); R12-R15 are the remaining callee-saved GPRs.
var pinnedLocalRegs = []Reg{R12, R13, R14, R15}

// pinnedFLocalRegs are XMM registers dedicated to hot float locals. XMM registers
// are all caller-saved, so (like the GP pinned locals) callers spill/reload them
// around calls. xmm0-11 stay in the operand pool.
var pinnedFLocalRegs = []Reg{12, 13, 14, 15}

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

// gpAllocPos returns r's index in gpAlloc, or -1 if r is not allocatable (RBX,
// RSP). Lower index = higher allocation priority. (RBP IS allocatable in this
// frameless backend.)
func gpAllocPos(r Reg) int {
	for i, a := range gpAlloc {
		if a == r {
			return i
		}
	}
	return -1
}
