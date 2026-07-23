package machinecode

import a64 "github.com/wago-org/wago/src/core/encoder/arm64"

// ARM64Compatibility selects the trust contract of an arm64 lowering.
// Managed exposes engine-owned values and checked memory only. FullAccess also
// exposes the raw AArch64 encoder and physical-register controls.
type ARM64Compatibility uint8

const (
	ARM64CompatibilityManaged ARM64Compatibility = iota + 1
	ARM64CompatibilityFullAccess
)

// ARM64Lowering lets a trusted plugin emit target-specific AArch64
// instructions during compilation. Wago does not interpret those instructions
// or attempt to make them equivalent to a lowering for another target.
type ARM64Lowering struct {
	Compatibility ARM64Compatibility
	Managed       func(ARM64ManagedContext) error
	Emit          func(ARM64Context) error
}

// ARM64ManagedContext exposes canonical scalar inputs, checked linear-memory
// addresses, and result placement. It intentionally defines no SIMD or
// cross-platform semantic operations.
type ARM64ManagedContext interface {
	InputI32(index int) (a64.Reg, error)
	InputCustom(index int) ([]a64.Reg, error)
	Release(reg a64.Reg)
	ReleaseGP(reg a64.Reg)
	ReleaseVector(reg a64.Reg)
	OutputI32(reg a64.Reg) error
	OutputCustom(regs ...a64.Reg) error
}

// ARM64Context exposes Wago's raw AArch64 encoder. Encoder().B is public, so a
// full-access plugin may append arbitrary instruction words. Registers must be
// allocated or reserved through this context before use.
type ARM64Context interface {
	ARM64ManagedContext
	Encoder() *a64.Asm
	AllocGP(exclude ...a64.Reg) a64.Reg
	AllocVector(exclude ...a64.Reg) a64.Reg
	ReserveGP(reg a64.Reg) error
	ReserveVector(reg a64.Reg) error
	MemoryBase() a64.Reg
	CheckedMemory(input int, offset uint32, size int) (base, index a64.Reg, disp int32, err error)
}
