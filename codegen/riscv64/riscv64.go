// Package riscv64 defines RV64G plugin code generation. Plugin packages select
// this lowering from a //go:build riscv64 source file.
package riscv64

import (
	"github.com/wago-org/wago/codegen"
	core "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type Asm = core.Asm
type Reg = core.Reg
type Cond = core.Cond
type Compatibility uint8

const (
	CompatibilityManaged Compatibility = iota + 1
	CompatibilityFullAccess
)

type Lowering struct {
	Compatibility Compatibility
	Managed       func(ManagedContext) error
	Emit          func(Context) error
}

func (*Lowering) Architecture() codegen.Architecture { return codegen.RISCV64 }

// ManagedContext owns temporary registers and typed input/output placements.
// A context is valid only during its lowering callback.
type ManagedContext interface {
	InputI32(index int) (Reg, error)
	// InputCustom loads the full value into GP cells, subject to register capacity.
	InputCustom(index int) ([]Reg, error)
	// InputCustomCell loads one 64-bit cell. Release it before loading another
	// when a value is larger than the available register bundle.
	InputCustomCell(index, cell int) (Reg, error)
	Release(reg Reg)
	ReleaseGP(reg Reg)
	OutputI32(reg Reg) error
	OutputCustom(regs ...Reg) error
	// OutputCustomCell commits one 64-bit cell. Each cell must be set exactly
	// once. The register may be released after the commit. Output reuses the
	// first input home; load a source cell before replacing it.
	OutputCustomCell(cell int, reg Reg) error
}

// Context grants trusted native code access. A lowering must not modify runtime
// reserved registers or retain its context or encoder after the callback.
type Context interface {
	ManagedContext
	Encoder() *Asm
	AllocGP(exclude ...Reg) Reg
	ReserveGP(reg Reg) error
	// Custom addresses are borrowed native frame homes. Output aliases the
	// first input home, so native lowerings must handle overlapping sources.
	// A lowering that declares an output address must initialize every output byte.
	InputCustomAddress(index int) (Reg, error)
	OutputCustomAddress() (Reg, error)
	MemoryBase() Reg
	CheckedMemory(input int, offset uint32, size int) (base, index Reg, disp int32, err error)
}

const (
	Zero = core.Zero
	RA   = core.RA
	SP   = core.SP
	X0   = core.X0
	X1   = core.X1
	X2   = core.X2
	X3   = core.X3
	X4   = core.X4
	X5   = core.X5
	X6   = core.X6
	X7   = core.X7
	X8   = core.X8
	X9   = core.X9
	X10  = core.X10
	X11  = core.X11
	X12  = core.X12
	X13  = core.X13
	X14  = core.X14
	X15  = core.X15
	X16  = core.X16
	X17  = core.X17
	X18  = core.X18
	X19  = core.X19
	X20  = core.X20
	X21  = core.X21
	X22  = core.X22
	X23  = core.X23
	X24  = core.X24
	X25  = core.X25
	X26  = core.X26
	X27  = core.X27
	X28  = core.X28
	X29  = core.X29
	X30  = core.X30
	X31  = core.X31
	EQ   = core.EQ
	NE   = core.NE
	LT   = core.LT
	GE   = core.GE
	LTU  = core.LTU
	GEU  = core.GEU
)
