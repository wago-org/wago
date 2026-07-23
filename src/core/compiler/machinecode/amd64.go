// Package machinecode defines the explicitly unsafe, target-specific compiler
// plugin boundary. Implementations are trusted code running inside Wago's JIT.
package machinecode

import x86 "github.com/wago-org/wago/src/core/encoder/amd64"

// AMD64Features declares CPU features required by a machine-code lowering.
type AMD64Features uint64

const (
	AMD64FeatureAVX2 AMD64Features = 1 << iota
	AMD64FeatureAVX512
)

// AMD64Compatibility selects the trust/portability contract of an amd64
// lowering. Managed exposes only checked typed operations. FullAccess exposes
// the encoder and physical-register controls and is not machine-code verified.
type AMD64Compatibility uint8

const (
	AMD64CompatibilityManaged AMD64Compatibility = iota + 1
	AMD64CompatibilityFullAccess
)

// AMD64Lowering lets a trusted plugin emit amd64 instructions directly.
// Emit runs during compilation, not while guest code is executing.
type AMD64Lowering struct {
	Compatibility AMD64Compatibility
	Features      AMD64Features
	Managed       func(AMD64ManagedContext) error
	Emit          func(AMD64Context) error
}

// AMD64ManagedContext is the safer machine-lowering surface. It provides
// canonical scalar inputs, checked memory, engine-owned SIMD lowering, and
// result placement without exposing the encoder or arbitrary registers.
type AMD64ManagedContext interface {
	InputI32(index int) (x86.Reg, error)
	Release(reg x86.Reg)
	ConstYMMRepeated128(lo, hi uint64) x86.Reg
	LoadYMM(input int, offset uint32) (x86.Reg, error)
	StoreYMM(input int, offset uint32, value x86.Reg) error
	SIMD256YMM(subopcode uint32, immediate []byte, inputs ...x86.Reg) (x86.Reg, error)
	LoadZMM(input int, offset uint32) (x86.Reg, error)
	StoreZMM(input int, offset uint32, value x86.Reg) error
	SIMD512ZMM(subopcode uint32, inputs ...x86.Reg) (x86.Reg, error)
	OutputI32(reg x86.Reg) error
}

// AMD64Context exposes Wago's real encoder and managed access to the surrounding
// compiler state. Encoder().B is intentionally public, so a trusted plugin may
// append arbitrary machine-code bytes. Using registers that were not allocated
// or reserved through this context may corrupt generated code.
type AMD64Context interface {
	AMD64ManagedContext
	Encoder() *x86.Asm
	AllocGP(exclude ...x86.Reg) x86.Reg
	AllocYMM(exclude ...x86.Reg) x86.Reg
	ReserveGP(reg x86.Reg) error
	ReserveYMM(reg x86.Reg) error
	MemoryBase() x86.Reg
	// CheckedMemory validates input+offset..+size against linear memory and
	// returns the base/index/disp tuple accepted by indexed encoder operations.
	CheckedMemory(input int, offset uint32, size int) (base, index x86.Reg, disp int32, err error)
}
