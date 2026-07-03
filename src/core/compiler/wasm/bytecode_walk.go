package wasm

// InstructionImmediate is the allocation-free classification returned while
// consuming an instruction's encoded immediates.
type InstructionImmediate struct {
	Kind           InstrKind
	Prefix         byte
	Subopcode      uint32
	Index          uint32
	Index2         uint32
	TouchesMemory  bool
	UsesBulkMemory bool
}

// ClassifyInstructionImmediate consumes the immediates for op from r and returns
// cheap metadata needed by bytecode walkers. The opcode byte itself must already
// have been consumed. It validates immediate encodings and skips vector
// immediates without allocating decoded Instruction payloads.
func ClassifyInstructionImmediate(r *Reader, op byte) (InstructionImmediate, error) {
	ir := &reader{data: r.data, pos: r.pos}
	_, imm, err := classifyExprOpAfterOpcode(ir, op)
	r.pos = ir.pos
	return imm, err
}

// SkipInstructionImmediate consumes the immediates for op from r. The opcode
// byte itself must already have been consumed. It validates immediate encodings
// and skips vector immediates without allocating decoded Instruction payloads.
// Structural opcodes (block/loop/if/else/end/try_table) are accepted and only
// their inline immediates are consumed.
func SkipInstructionImmediate(r *Reader, op byte) error {
	_, err := ClassifyInstructionImmediate(r, op)
	return err
}
