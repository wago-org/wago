// Package arm64 exposes Wago's raw AArch64 encoder vocabulary to trusted
// compiler plugins. Asm is the same encoder instance returned by
// wago.ARM64LoweringContext.Encoder.
package arm64

import core "github.com/wago-org/wago/src/core/encoder/arm64"

type Asm = core.Asm
type Reg = core.Reg
type Cond = core.Cond

const (
	X0  = core.X0
	X1  = core.X1
	X2  = core.X2
	X3  = core.X3
	X4  = core.X4
	X5  = core.X5
	X6  = core.X6
	X7  = core.X7
	X8  = core.X8
	X9  = core.X9
	X10 = core.X10
	X11 = core.X11
	X12 = core.X12
	X13 = core.X13
	X14 = core.X14
	X15 = core.X15
	X16 = core.X16
	X17 = core.X17
	X18 = core.X18
	X19 = core.X19
	X20 = core.X20
	X21 = core.X21
	X22 = core.X22
	X23 = core.X23
	X24 = core.X24
	X25 = core.X25
	X26 = core.X26
	X27 = core.X27
	X28 = core.X28
	X29 = core.X29
	X30 = core.X30
	XZR = core.XZR
	FP  = core.FP
	LR  = core.LR
	SP  = core.SP
	ZR  = core.ZR

	CondEQ = core.CondEQ
	CondNE = core.CondNE
	CondCS = core.CondCS
	CondCC = core.CondCC
	CondMI = core.CondMI
	CondPL = core.CondPL
	CondVS = core.CondVS
	CondVC = core.CondVC
	CondHI = core.CondHI
	CondLS = core.CondLS
	CondGE = core.CondGE
	CondLT = core.CondLT
	CondGT = core.CondGT
	CondLE = core.CondLE
	CondAL = core.CondAL
	CondNV = core.CondNV
)
