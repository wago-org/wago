// Package amd64 defines AMD64 plugin code generation. Plugin packages should
// use it from a //go:build amd64 source file.
package amd64

import (
	"github.com/wago-org/wago/codegen"
	core "github.com/wago-org/wago/src/core/encoder/amd64"
)

type Asm = core.Asm
type Reg = core.Reg
type Cond = core.Cond

// Features declares CPU features required by a lowering.
type Features uint64

const (
	FeatureAVX2 Features = 1 << iota
	FeatureAVX512
)

// Compatibility selects the trust contract of an AMD64 lowering.
type Compatibility uint8

const (
	CompatibilityManaged Compatibility = iota + 1
	CompatibilityFullAccess
)

// Lowering emits AMD64 instructions during compilation.
type Lowering struct {
	Compatibility Compatibility
	Features      Features
	Managed       func(ManagedContext) error
	Emit          func(Context) error
}

func (*Lowering) Architecture() codegen.Architecture { return codegen.AMD64 }

// ManagedContext exposes inputs, checked memory, and result placement without
// exposing the raw encoder.
type ManagedContext interface {
	InputI32(index int) (Reg, error)
	InputCustom(index int) ([]Reg, error)
	Release(reg Reg)
	ReleaseGP(reg Reg)
	ReleaseVector(reg Reg)
	ConstYMMRepeated128(lo, hi uint64) Reg
	LoadYMM(input int, offset uint32) (Reg, error)
	StoreYMM(input int, offset uint32, value Reg) error
	LoadZMM(input int, offset uint32) (Reg, error)
	StoreZMM(input int, offset uint32, value Reg) error
	OutputI32(reg Reg) error
	OutputCustom(regs ...Reg) error
}

// Context exposes the raw AMD64 encoder and physical-register controls.
type Context interface {
	ManagedContext
	Encoder() *Asm
	AllocGP(exclude ...Reg) Reg
	AllocYMM(exclude ...Reg) Reg
	ReserveGP(reg Reg) error
	ReserveYMM(reg Reg) error
	MemoryBase() Reg
	CheckedMemory(input int, offset uint32, size int) (base, index Reg, disp int32, err error)
}

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
