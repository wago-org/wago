package arm64

import "github.com/wago-org/wago/src/core/codeimage"

// CompiledModule is the output of a code generator built on this encoder: the
// concatenated machine code for all local functions plus each function's entry
// offset. The arm64 package is an AArch64 instruction *encoder* only (Asm); the
// wasm→native code generator lives in backend/railshot/arm64 and returns a
// *CompiledModule. Mirrors encoder/amd64.CompiledModule.
type CompiledModule struct {
	// GCDeadInductionLayouts proves the complete function charges max(n,1)
	// nonescaping pointer-free objects and returns i32(n*(n-1)/2).
	// Low/high words hold object size/alignment. Zero means no proof.
	// Compiler-private capability: excluded from serialized compiled artifacts.
	GCDeadInductionLayouts []uint64
	// GCI31Inductions marks compiler-proven whole unsigned i31 induction sums.
	GCI31Inductions []uint64
	// RequiredAMD64Features records optional instructions emitted by the AMD64 backend.
	RequiredAMD64Features uint32
	RequiresARM64LSE      bool // optional LSE instructions emitted by the arm64 backend

	Code           []byte          // all local functions concatenated, 16-byte aligned
	CodeImage      codeimage.Image // owns Code on the serial direct-image path
	Entry          []int           // Entry[localFuncIdx] = byte offset in Code
	InternalEntry  []int           // register-ABI internal entry offset (== Entry[i] when none)
	DirectPrepared []uint64        // reserved for parity with AMD64 prepared-entry metadata
	// DirectPreparedLight marks direct entries whose compiler proof limits
	// clobbers to caller-saved registers, allowing a smaller Go boundary thunk.
	DirectPreparedLight []uint64
	// DirectPreparedBounded marks statically bounded native segments. Entries
	// with GC helpers require the checked helper-yielding host loop, not direct entry.
	DirectPreparedBounded []uint64
	// PreparedIsolatedTables reports that every table is local, unexported,
	// never mutated, and contains only local function descriptors. Runtime entry
	// selection may then treat the table descriptor arena as instance-private,
	// read-only state.
	PreparedIsolatedTables bool
	RequiresBMI2           bool  // always false on arm64; keeps backend result metadata uniform
	RequiresBitCount       uint8 // always zero on arm64
	RequiresAVX2           bool  // always false on arm64; keeps backend result metadata uniform
	RequiresAVX512         bool  // always false on arm64; keeps backend result metadata uniform
}
