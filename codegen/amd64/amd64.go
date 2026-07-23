// Package amd64 exposes Wago's machine-code encoder vocabulary to trusted
// compiler plugins. Asm is the same encoder instance returned by
// wago.AMD64LoweringContext.Encoder.
package amd64

import core "github.com/wago-org/wago/src/core/encoder/amd64"

type Asm = core.Asm
type Reg = core.Reg
type Cond = core.Cond

const (
	RAX = core.RAX
	RCX = core.RCX
	RDX = core.RDX
	RBX = core.RBX
	RSP = core.RSP
	RBP = core.RBP
	RSI = core.RSI
	RDI = core.RDI
	R8  = core.R8
	R9  = core.R9
	R10 = core.R10
	R11 = core.R11
	R12 = core.R12
	R13 = core.R13
	R14 = core.R14
	R15 = core.R15

	CondE  = core.CondE
	CondNE = core.CondNE
	CondB  = core.CondB
	CondAE = core.CondAE
	CondBE = core.CondBE
	CondA  = core.CondA
	CondL  = core.CondL
	CondGE = core.CondGE
	CondLE = core.CondLE
	CondG  = core.CondG
	CondP  = core.CondP
	CondNP = core.CondNP
	CondS  = core.CondS
)
