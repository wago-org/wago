package riscv64

// CompiledModule is the output of a code generator built on this encoder: the
// concatenated machine code for all local functions plus each function's entry
// offsets. The wasm-to-native code generator will live in
// backend/railshot/riscv64; this package remains an instruction writer only.
type CompiledModule struct {
	Code          []byte // all local functions concatenated, 16-byte aligned
	Entry         []int  // wrapper-ABI entry offsets
	InternalEntry []int  // register-ABI entry offsets (or Entry when unavailable)
}
