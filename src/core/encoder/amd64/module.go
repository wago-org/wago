package amd64

import "github.com/wago-org/wago/src/core/codeimage"

// CompiledModule is the output of a code generator built on this encoder: the
// concatenated machine code for all local functions plus each function's entry
// offset into it. The amd64 package is now an x86-64 instruction *encoder* only
// (the Asm type); the wasm→native code generator lives in backend/railshot, which
// drives this encoder and returns a *CompiledModule.
type CompiledModule struct {
	Code  []byte // all local functions concatenated, 16-byte aligned
	Entry []int  // Entry[localFuncIdx] = byte offset of that function in Code

	// CodeImage owns Code when serial module compilation emitted directly into
	// an off-heap image. Parallel and hand-built compiler results leave it nil.
	CodeImage codeimage.Image

	// InternalEntry[localFuncIdx] = byte offset of the function's register-ABI
	// internal entry (== Entry[i] when the function has none). Lets indirect
	// calls with a register-ABI-compatible signature bypass the wrapper adapter.
	InternalEntry []int

	// DirectPrepared marks small register-ABI internal entries constrained to
	// caller-saved GPRs and needing only RBX (linMem) from the host trampoline.
	// The optional bitset uses one bit per local function.
	DirectPrepared []uint64

	// PreparedIsolatedTables reports that every table is local, unexported,
	// never mutated, and contains only local function descriptors. Runtime entry
	// selection may then treat the table descriptor arena as instance-private,
	// read-only state.
	PreparedIsolatedTables bool

	// RequiresBMI2 reports that Code contains a BMI2 instruction.
	RequiresBMI2 bool

	// RequiresAVX2 reports that Code contains AVX2/YMM instructions selected by
	// the backend (including plugin-provided portable intrinsics).
	RequiresAVX2 bool

	// RequiresAVX512 reports that Code contains an AVX-512/ZMM lowering.
	RequiresAVX512 bool
}
