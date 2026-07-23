package arm64

// CompiledModule is the output of a code generator built on this encoder: the
// concatenated machine code for all local functions plus each function's entry
// offset. The arm64 package is an AArch64 instruction *encoder* only (Asm); the
// wasm→native code generator lives in backend/railshot/arm64 and returns a
// *CompiledModule. Mirrors encoder/amd64.CompiledModule.
type CompiledModule struct {
	Code           []byte // all local functions concatenated, 16-byte aligned
	Entry          []int  // Entry[localFuncIdx] = byte offset in Code
	InternalEntry  []int  // register-ABI internal entry offset (== Entry[i] when none)
	RequiresAVX2   bool   // always false on arm64; keeps backend result metadata uniform
	RequiresAVX512 bool   // always false on arm64; keeps backend result metadata uniform
}
