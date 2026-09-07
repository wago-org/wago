package arm64

import "github.com/wago-org/wago/src/core/codeimage"

// CompiledModule is the output of a code generator built on this encoder: the
// concatenated machine code for all local functions plus each function's entry
// offset. The arm64 package is an AArch64 instruction *encoder* only (Asm); the
// wasm→native code generator lives in backend/railshot/arm64 and returns a
// *CompiledModule. Mirrors encoder/amd64.CompiledModule.
type CompiledModule struct {
	Code           []byte          // all local functions concatenated, 16-byte aligned
	CodeImage      codeimage.Image // owns Code on the serial direct-image path
	Entry          []int           // Entry[localFuncIdx] = byte offset in Code
	InternalEntry  []int           // register-ABI internal entry offset (== Entry[i] when none)
	DirectPrepared []uint64        // reserved for parity with AMD64 prepared-entry metadata
	// PreparedIsolatedTables reports that every table is local, unexported,
	// never mutated, and contains only local function descriptors. Runtime entry
	// selection may then treat the table descriptor arena as instance-private,
	// read-only state.
	PreparedIsolatedTables bool
	RequiresBMI2           bool // always false on arm64; keeps backend result metadata uniform
	RequiresAVX2           bool // always false on arm64; keeps backend result metadata uniform
	RequiresAVX512         bool // always false on arm64; keeps backend result metadata uniform
}
