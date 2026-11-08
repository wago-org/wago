package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// LoopExpression owns the semantics of one expression in a bounded candidate.
// Children index that candidate's expression pool, never allocator descriptors.
// Type and constant bits retain source arithmetic; memory immediates belong in
// separate ordered access records. No expression survives candidate emission.
type LoopExpression struct {
	Bits        uint64
	Left, Right int
	PC          uint32
	Op          byte
	Type        wasm.ValType
}
