// Package arm64 defines ARM64 plugin code generation. Plugin packages should
// use it from a //go:build arm64 source file.
package arm64

import (
	"github.com/wago-org/wago/codegen"
	core "github.com/wago-org/wago/src/core/encoder/arm64"
)

type Asm = core.Asm
type Reg = core.Reg
type Cond = core.Cond

// Compatibility selects the trust contract of an ARM64 lowering.
type Compatibility uint8

const (
	CompatibilityManaged Compatibility = iota + 1
	CompatibilityFullAccess
)

// Lowering emits ARM64 instructions during compilation.
type Lowering struct {
	Compatibility Compatibility
	Managed       func(ManagedContext) error
	Emit          func(Context) error
}

func (*Lowering) Architecture() codegen.Architecture { return codegen.ARM64 }

// ManagedContext exposes inputs and result placement without the raw encoder.
type ManagedContext interface {
	InputI32(index int) (Reg, error)
	InputCustom(index int) ([]Reg, error)
	Release(reg Reg)
	ReleaseGP(reg Reg)
	ReleaseVector(reg Reg)
	OutputI32(reg Reg) error
	OutputCustom(regs ...Reg) error
}

// Context exposes the raw ARM64 encoder and physical-register controls.
type Context interface {
	ManagedContext
	Encoder() *Asm
	AllocGP(exclude ...Reg) Reg
	AllocVector(exclude ...Reg) Reg
	ReserveGP(reg Reg) error
	ReserveVector(reg Reg) error
	MemoryBase() Reg
	CheckedMemory(input int, offset uint32, size int) (base, index Reg, disp int32, err error)
}

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
