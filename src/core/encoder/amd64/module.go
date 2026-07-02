package amd64

// CompiledModule is the output of a code generator built on this encoder: the
// concatenated machine code for all local functions plus each function's entry
// offset into it. The amd64 package is now an x86-64 instruction *encoder* only
// (the Asm type); the wasm→native code generator lives in backend/railshot, which
// drives this encoder and returns a *CompiledModule.
type CompiledModule struct {
	Code  []byte // all local functions concatenated, 16-byte aligned
	Entry []int  // Entry[localFuncIdx] = byte offset of that function in Code

	// InternalEntry[localFuncIdx] = byte offset of the function's register-ABI
	// internal entry (== Entry[i] when the function has none). Lets indirect
	// calls with a register-ABI-compatible signature bypass the wrapper adapter.
	InternalEntry []int
}
