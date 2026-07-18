//go:build riscv64

// Package riscv64 is wago's native RV64 railshot code generator. It consumes
// the same validated wasm and valent-block state as the other railshot backends,
// but instruction selection is expressed in RISC-V terms: comparisons produce
// integer values, branches compare registers directly, and addressing modes are
// synthesized explicitly.
package riscv64

import rv "github.com/wago-org/wago/src/core/encoder/riscv64"

type Reg = rv.Reg

// Cond is a backend semantic relation. machine lowers it to one or more native
// RISC-V instructions; it is deliberately not part of encoder/riscv64 because
// composite relations and floating unordered tests are not ISA branch codes.
type Cond uint8

const (
	condE Cond = iota
	condNE
	condB  // unsigned <
	condAE // unsigned >=
	condBE // unsigned <=
	condA  // unsigned >
	condL  // signed <
	condGE // signed >=
	condLE // signed <=
	condG  // signed >
	condS  // negative
	condNS // non-negative
	condVS // unordered / overflow
	condVC // ordered / no overflow
)

func (c Cond) Invert() Cond {
	switch c {
	case condE:
		return condNE
	case condNE:
		return condE
	case condB:
		return condAE
	case condAE:
		return condB
	case condBE:
		return condA
	case condA:
		return condBE
	case condL:
		return condGE
	case condGE:
		return condL
	case condLE:
		return condG
	case condG:
		return condLE
	case condS:
		return condNS
	case condNS:
		return condS
	case condVS:
		return condVC
	case condVC:
		return condVS
	default:
		panic("riscv64: invalid condition")
	}
}

// Logical backend names preserve the architecture-parallel call and allocator
// algorithms while mapping to the actual RV64 psABI register file. In particular
// X0..X7 are the eight integer argument/result registers A0..A7.
const (
	X0  Reg = rv.A0
	X1  Reg = rv.A1
	X2  Reg = rv.A2
	X3  Reg = rv.A3
	X4  Reg = rv.A4
	X5  Reg = rv.A5
	X6  Reg = rv.A6
	X7  Reg = rv.A7
	X8  Reg = rv.T0
	X9  Reg = rv.T1
	X10 Reg = rv.T2
	X11 Reg = rv.T3
	X12 Reg = rv.T4
	X13 Reg = rv.S0
	X14 Reg = rv.S1
	X15 Reg = rv.S2
	X16 Reg = rv.T5 // address/materialization scratch; never allocated
	X17 Reg = rv.T6 // far branch/call scratch; never allocated
	X18 Reg = rv.GP // non-allocatable compatibility name
	X19 Reg = rv.S3
	X20 Reg = rv.S4
	X21 Reg = rv.S5
	X22 Reg = rv.S6
	X23 Reg = rv.S7
	// X24/X25 are compatibility names for the module-global save set. They
	// alias the physical S6/S7 registers used by X22/X23; regMask removes those
	// physical registers from every allocator pool when a module global is pinned.
	X24 Reg = rv.S6
	X25 Reg = rv.S7
	X26 Reg = rv.S9
	X27 Reg = rv.S8 // mem-size role
	X28 Reg = rv.GP

	linMemReg = X26
	FP        = rv.S0
	LR        = rv.RA
	SP        = rv.SP
	ZR        = rv.Zero
)

// The pool excludes GP, TP, SP, RA, Go CTXT/X26, Go g/X27, linMem/S9, and the
// fixed T5/T6 backend scratch pair. Caller-saved registers are preferred, then
// the callee-saved pin range, with A1/A0 retained as last-resort/result scratch.
var gpAlloc = []Reg{
	X9, X10, X11, X12, X13, X14, X15, X8, X7, X6, X5, X4, X3, X2,
	X19, X20, X21, X22, X23,
	X1, X0,
}

const numScratchGP = 2

var pinnedLocalRegs = []Reg{X19, X20, X21, X22, X23}

// The backend does not make callee-saved floating registers part of its internal
// Wasm call ABI. Pinned float locals are therefore restricted to call-free
// functions; call-making functions spill float values explicitly. The full
// F0..F31 file remains available for transient allocation.
const basePinnedFLocalRegs = 0
const callFreePinnedFLocalRegs = 15

var pinnedFLocalRegs = []Reg{
	8, 9, 10, 11, 12, 13, 14,
	16, 17, 18, 19, 20, 21, 22, 23,
	24, 25, 26, 27, 28, 29, 30, 31,
	4, 5, 6, 7,
}

var fpAllocRegs = []Reg{
	0, 1, 2, 3, 4, 5, 6, 7,
	8, 9, 10, 11, 12, 13, 14, 15,
	16, 17, 18, 19, 20, 21, 22, 23,
	24, 25, 26, 27, 28, 29, 30, 31,
}

func isScratchGP(r Reg) bool {
	for _, s := range gpAlloc[len(gpAlloc)-numScratchGP:] {
		if s == r {
			return true
		}
	}
	return false
}

func gpAllocPos(r Reg) int {
	for i, a := range gpAlloc {
		if a == r {
			return i
		}
	}
	return -1
}
